package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

// EditorSabyLink is read-only mirror metadata. It deliberately stays separate
// from the editable saved plan: reading a card must never replace draft choices
// or turn a display code into the identifier used by stock/integrations.
type EditorSabyLink struct {
	ID       string         `json:"id"`
	Code     string         `json:"code"`
	Category EditorCategory `json:"category"`
	Folder   EditorCategory `json:"folder"`
}

func (r *PostgresRepository) WithEditorSabyMetadata(reader catalogeditor.SabyMetadataReader, environment string) *PostgresRepository {
	r.editorSabyMetadata = reader
	r.editorEnvironment = environment
	return r
}

func (r *PostgresRepository) readEditorSabyLinks(ctx context.Context, productID int64) (map[int64]EditorSabyLink, error) {
	// Legacy imports sometimes retain a numeric integration ID while the local
	// mirror is indexed by a business code. Resolve only explicit aliases belonging
	// to this variant, and decline ambiguous matches. An exact ID always wins.
	rows, err := r.pool.Query(ctx, `
	 WITH variants AS (
	  SELECT v.id,COALESCE(NULLIF(v.saby_id,''),CASE WHEN cardinality(e.ids)=1 THEN e.ids[1] ELSE '' END) saby_id,e.codes,e.aliases
	  FROM product_variants v LEFT JOIN LATERAL (
	   SELECT COALESCE(array_agg(DISTINCT external_id) FILTER(WHERE id_type='id'),'{}'::text[]) ids,
	    COALESCE(array_agg(DISTINCT external_id) FILTER(WHERE id_type='code'),'{}'::text[]) codes,
	    COALESCE(array_agg(DISTINCT external_id) FILTER(WHERE id_type IN ('id','code','alias')),'{}'::text[]) aliases
	   FROM product_external_ids e WHERE e.product_id=v.product_id AND e.provider='saby' AND e.status='active'
	    AND (e.variant_id=v.id OR (e.variant_id IS NULL AND NOT EXISTS(SELECT 1 FROM product_variants other WHERE other.product_id=v.product_id AND other.id<>v.id)))
	  ) e ON TRUE WHERE v.product_id=$1
	 )
	 SELECT v.id,v.saby_id,COALESCE(NULLIF(n.code,''),CASE WHEN cardinality(v.codes)=1 THEN v.codes[1] ELSE '' END),
	  COALESCE(n.folder_saby_id,''),
	  COALESCE(NULLIF(array_to_string(f.path,' / '),''),f.name,''),v.aliases
	 FROM variants v LEFT JOIN LATERAL (
	  SELECT candidate.* FROM (
	   SELECT n.*,CASE WHEN n.saby_id=v.saby_id THEN 0 ELSE 1 END priority,
	    count(*) OVER(PARTITION BY n.saby_id=v.saby_id) matches
	   FROM saby_nomenclature n WHERE n.missing_since IS NULL AND
	    (n.saby_id=v.saby_id OR n.saby_id=ANY(v.aliases) OR n.code=ANY(v.codes) OR n.external_ids && (ARRAY[v.saby_id]||v.aliases))
	  ) candidate WHERE candidate.matches=1 ORDER BY candidate.priority LIMIT 1
	 ) n ON TRUE
	 LEFT JOIN saby_catalog_folders f ON f.saby_id=n.folder_saby_id AND f.missing_since IS NULL
	 WHERE v.saby_id<>'' ORDER BY v.id`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := map[int64]EditorSabyLink{}
	identities := map[int64]catalogeditor.SabyIdentity{}
	for rows.Next() {
		var variantID int64
		var link EditorSabyLink
		var aliases []string
		if err = rows.Scan(&variantID, &link.ID, &link.Code, &link.Folder.ID, &link.Folder.Name, &aliases); err != nil {
			return nil, err
		}
		// section_path is a folder ancestry, not a nomenclature category. The
		// current Saby mirror has no category ID: leave it unknown, never infer
		// it from a site category, a folder name or a category dictionary.
		links[variantID] = link
		identities[variantID] = catalogeditor.SabyIdentity{ID: link.ID, Code: link.Code, Aliases: aliases}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// One shared deadline bounds a card with many variants. Unavailable Saby
	// metadata never prevents opening the editor or changes its saved plan.
	metadataCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for id, identity := range identities {
		metadata, ok := r.editorSabyMetadataSnapshot(metadataCtx, identity)
		if !ok {
			continue
		}
		link := links[id]
		link.Code = metadata.Code
		if metadata.Category.ID != "" && metadata.Category.Name != "" {
			link.Category = metadata.Category
		}
		if metadata.Folder.ID != "" && metadata.Folder.Name != "" {
			link.Folder = metadata.Folder
		}
		links[id] = link
	}
	return links, nil
}

func (r *PostgresRepository) editorSabyMetadataSnapshot(ctx context.Context, identity catalogeditor.SabyIdentity) (catalogeditor.SabyMetadata, bool) {
	var metadata catalogeditor.SabyMetadata
	if ctx.Err() != nil {
		return metadata, false
	}
	raw, err := r.CachedEditorReference(ctx, "saby", "linked-card", identity.ID)
	if err != nil {
		return metadata, false
	}
	if len(raw) == 0 && r.editorPublicationMode == "mock" && (r.editorEnvironment == "staging" || r.editorEnvironment == "development") {
		// A deliberately imported, verified preview snapshot remains usable
		// without production credentials. Keep its real fetched_at unchanged.
		_ = r.pool.QueryRow(ctx, `SELECT payload FROM catalog_editor_references WHERE channel='saby' AND kind=$1 AND scope=$2`, editorReferenceKind(r.editorPublicationMode, "linked-card"), identity.ID).Scan(&raw)
	}
	if len(raw) > 0 && json.Unmarshal(raw, &metadata) == nil && completeEditorSabyMetadata(metadata) && catalogeditor.ValidateSabyMetadata(identity, metadata) == nil {
		return metadata, true
	}
	if r.editorSabyMetadata == nil || !r.editorSabyMetadata.Configured() {
		return catalogeditor.SabyMetadata{}, false
	}
	metadata, err = r.editorSabyMetadata.ReadSabyEditorMetadata(ctx, identity)
	if err != nil || catalogeditor.ValidateSabyMetadata(identity, metadata) != nil {
		return catalogeditor.SabyMetadata{}, false
	}
	if raw, err = json.Marshal(metadata); err == nil && completeEditorSabyMetadata(metadata) {
		// A failed cache write does not discard an already verified readback.
		_ = r.CacheEditorReference(ctx, "saby", "linked-card", identity.ID, raw)
	}
	return metadata, true
}

func completeEditorSabyMetadata(metadata catalogeditor.SabyMetadata) bool {
	return metadata.Category.ID != "" && metadata.Category.Name != "" && metadata.Folder.ID != "" && metadata.Folder.Name != ""
}
