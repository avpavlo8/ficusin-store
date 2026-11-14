package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"github.com/jackc/pgx/v5"
)

type EditorSyncResult struct {
	Queued   int      `json:"queued"`
	Skipped  int      `json:"skipped"`
	Warnings []string `json:"warnings"`
}

// SyncExistingEditorPlan never consumes an editor draft. Only an established
// publication supplies vendor configuration; only saved content is refreshed.
func (r *PostgresRepository) SyncExistingEditorPlan(ctx context.Context, actor Actor, productID int64) (EditorSyncResult, error) {
	out := EditorSyncResult{Warnings: []string{}}
	if !Can(actor.Role, PermissionProductsEdit) {
		return out, ErrForbidden
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var name, description string
	if err = tx.QueryRow(ctx, `SELECT name,description FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&name, &description); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM product_variants WHERE product_id=$1 AND archived_at IS NULL ORDER BY id FOR UPDATE`, productID)
	if err != nil {
		return out, err
	}
	var variants []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		variants = append(variants, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	skip := func(channel, reason string) {
		out.Skipped++
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s: %s", channel, reason))
	}
	for _, variant := range variants {
		var sabyID string
		var attrs map[string]any
		if err = tx.QueryRow(ctx, `SELECT COALESCE(saby_id,''),COALESCE((SELECT jsonb_object_agg(d.code,a.value) FROM variant_attribute_values a JOIN attribute_definitions d ON d.id=a.attribute_id WHERE a.variant_id=v.id),'{}'::jsonb) FROM product_variants v WHERE id=$1`, variant).Scan(&sabyID, &attrs); err != nil {
			return out, err
		}
		links, e := editorLinkedChannels(ctx, tx, variant, r.editorPublicationMode)
		if e != nil {
			return out, e
		}
		links["saby"] = sabyID
		for _, channel := range []string{"saby", "wb", "ozon"} {
			linked := strings.TrimSpace(links[channel])
			if linked == "" {
				continue
			}
			if linked == "!ambiguous" {
				skip(channel, "несколько связей; владелец должен проверить настройку")
				continue
			}
			if priceErr := ce.GuardContentPriceConflict(ctx, tx, variant, r.editorPublicationMode); priceErr != nil {
				if !errors.Is(priceErr, ce.ErrPricePublicationBusy) {
					return out, priceErr
				}
				skip(channel, "подтверждённая цена ещё проверяется; контент сохранён на сайте, повторите синхронизацию после завершения цены")
				continue
			}
			var status, mode, oldHash string
			var raw, pending []byte
			var progress ce.Progress
			err = tx.QueryRow(ctx, `SELECT status,execution_mode,fingerprint,payload,pending_payload,progress FROM catalog_editor_publications WHERE variant_id=$1 AND channel=$2 FOR UPDATE`, variant, channel).Scan(&status, &mode, &oldHash, &raw, &pending, &progress)
			if err == pgx.ErrNoRows {
				skip(channel, "настройка обмена отсутствует; контент сохранён, настройку должен завершить владелец")
				continue
			}
			if err != nil {
				return out, err
			}
			if mode != r.editorPublicationMode || (mode != "live" && mode != "mock") {
				skip(channel, "режим обмена не совпадает с сохранённой настройкой")
				continue
			}
			var prior ce.Request
			if err = json.Unmarshal(raw, &prior); err != nil {
				return out, err
			}
			if prior.ProductID != productID || prior.VariantID != variant {
				skip(channel, "сохранённая настройка не принадлежит этому варианту")
				continue
			}
			finished := status == "created" || status == "synced" || status == "linked" || status == "simulated"
			// A nonterminal creation/link or an ambiguous failure belongs to its
			// original owner operation. Content saving must not restart it.
			if !finished && (prior.Action != "update" || status == "failed" || status == "needs_attention" || progress.Ambiguous) {
				skip(channel, "предыдущая отправка требует проверки; новые данные сохранены на сайте")
				continue
			}
			if !editorSyncTargetMatches(channel, linked, prior, progress, finished) {
				skip(channel, "связь изменилась; владелец должен проверить настройку")
				continue
			}
			base := raw
			if len(pending) > 0 {
				var next ce.Request
				if json.Unmarshal(pending, &next) != nil || next.ProductID != productID || next.VariantID != variant || next.Action != "update" || next.ExistingID != linked {
					skip(channel, "ожидается изменение связи или настройки владельцем")
					continue
				}
				base = pending
			}
			updated, e := editorSyncContent(ctx, tx, base, productID, variant, channel, linked, name, description, attrs)
			if e != nil {
				return out, e
			}
			hash := editorPublicationFingerprint(channel, updated)
			if hash == oldHash && len(pending) == 0 {
				continue
			}
			if len(pending) > 0 && hash == editorPublicationFingerprint(channel, pending) {
				continue
			}
			if status == "processing" || status == "queued" {
				_, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET pending_payload=$3,pending_fingerprint=$4,updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, variant, channel, updated, hash)
			} else {
				_, err = tx.Exec(ctx, `UPDATE catalog_editor_publications SET payload=$3,fingerprint=$4,pending_payload=NULL,pending_fingerprint=NULL,progress='{}'::jsonb,status='prepared',message='Сохранённый контент ожидает обновления',next_attempt_at=NOW(),updated_at=NOW() WHERE variant_id=$1 AND channel=$2`, variant, channel, updated, hash)
			}
			if err != nil {
				return out, err
			}
			out.Queued++
		}
	}
	if err = insertAudit(ctx, tx, actor, "catalog.editor.sync-existing", "product", fmt.Sprint(productID), nil, out); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

func editorSyncTargetMatches(channel, linked string, request ce.Request, progress ce.Progress, finished bool) bool {
	if request.Action == "update" {
		return request.ExistingID == linked
	}
	if !finished {
		return false
	}
	if channel == "ozon" {
		return progress.IDs.OfferID == linked
	}
	return progress.IDs.ProductID == linked
}

func editorSyncContent(ctx context.Context, tx pgx.Tx, raw []byte, productID, variant int64, channel, linked, name, description string, attrs map[string]any) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	oldName, oldDescription := payload["name"], payload["description"]
	payload["action"], payload["existingId"] = "update", linked
	payload["attributes"], payload["name"], payload["description"] = attrs, name, description
	if channel == "saby" {
		payload["name"] = editorSabyName(name, attrs)
		payload["sabyId"] = linked
	}
	// Explicit vendor copy remains an override. Previously inherited copy
	// follows the site's saved content without changing vendor configuration.
	if m, ok := payload["marketplace"].(map[string]any); ok && channel != "saby" {
		if m["name"] == oldName {
			m["name"] = name
		}
		if m["description"] == oldDescription {
			m["description"] = description
		}
	}
	images := []string{}
	seen := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT COALESCE(m.large_url,p.object_key) FROM product_media p LEFT JOIN media_mirror m ON m.source_url=p.object_key WHERE p.product_id=$1 AND (p.variant_id IS NULL OR p.variant_id=$2) ORDER BY (p.variant_id IS NULL AND p.is_primary<>0) DESC,p.is_primary DESC,p.sort_order,p.id`, productID, variant)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var image string
		if err = rows.Scan(&image); err != nil {
			rows.Close()
			return nil, err
		}
		if (strings.HasPrefix(image, "https://") || strings.HasPrefix(image, "http://")) && !editorVideoURL(image) && !seen[image] {
			images = append(images, image)
			seen[image] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	payload["images"] = images
	if channel != "saby" {
		var season string
		var length, width, height, weight float64
		err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM settings WHERE key='cdek.packaging_season'),'summer'),COALESCE(shipping_package_dimension($1,'package_length_cm'),0),COALESCE(shipping_package_dimension($1,'package_width_cm'),0),COALESCE(shipping_package_dimension($1,'package_height_cm'),0),COALESCE(variant_numeric_attribute($1,'package_weight_grams'),0)`, variant).Scan(&season, &length, &width, &height, &weight)
		if err != nil {
			return nil, err
		}
		payload["packaging"] = ce.Packaging{Season: season, Length: length, Width: width, Height: height, Weight: weight}
	}
	return json.Marshal(payload)
}
