package integration

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

type sabyEditorGalleryDelta struct {
	Added      []string `json:"added,omitempty"`
	RemovedIDs []string `json:"removedIds,omitempty"`
}

func (d sabyEditorGalleryDelta) changed() bool {
	return len(d.RemovedIDs)+len(d.Added) > 0
}

func (p *SabyEditorPublisher) sabyEditorGalleryChanges(ctx context.Context, raw json.RawMessage, baseline *ce.SitePhotoBaseline, after []string) (sabyEditorGalleryDelta, error) {
	delta := sabyEditorGalleryDelta{}
	removed, added := catalogSitePhotoDelta(baseline, after)
	if len(removed)+len(added) == 0 {
		return delta, nil
	}
	set, err := parseSabyEditorNativeRecordSet(raw)
	if err != nil {
		return delta, err
	}
	var candidates, ids []string
	for i := range set.rows {
		if !sabyEditorImageEditable(set, i) {
			continue
		}
		var id, source string
		_ = json.Unmarshal(set.cell(i, "id"), &id)
		_ = json.Unmarshal(set.cell(i, "fullUrl"), &source)
		if !strings.HasPrefix(source, "https://") {
			_ = json.Unmarshal(set.cell(i, "url"), &source)
		}
		if id != "" && strings.HasPrefix(source, "https://") {
			candidates = append(candidates, source)
			ids = append(ids, id)
		}
	}
	changes, err := catalogGalleryDelta(ctx, p.ImageClient, candidates, baseline, after)
	if err != nil {
		return delta, err
	}
	for _, i := range changes.Removed {
		delta.RemovedIDs = append(delta.RemovedIDs, ids[i])
	}
	delta.Added = changes.Added
	return delta, nil
}

func sabyEditorImageEditable(set *sabyEditorNativeRecordSet, i int) bool {
	var canDelete, canEdit, isMarket bool
	deleteKnown := json.Unmarshal(set.cell(i, "canDelete"), &canDelete) == nil
	editKnown := json.Unmarshal(set.cell(i, "canEdit"), &canEdit) == nil
	_ = json.Unmarshal(set.cell(i, "isMarketImage"), &isMarket)
	return deleteKnown && editKnown && canDelete && canEdit && !isMarket
}

func (d sabyEditorGalleryDelta) retainedRows(set *sabyEditorNativeRecordSet) ([][]json.RawMessage, error) {
	rows := make([][]json.RawMessage, 0, len(set.rows))
	for i, row := range set.rows {
		var id string
		_ = json.Unmarshal(set.cell(i, "id"), &id)
		if slices.Contains(d.RemovedIDs, id) {
			if !sabyEditorImageEditable(set, i) {
				return nil, errors.New("Фото СБИС больше недоступно для удаления. Проверьте галерею")
			}
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}
