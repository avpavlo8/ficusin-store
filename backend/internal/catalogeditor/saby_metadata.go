package catalogeditor

import (
	"context"
	"errors"
	"strings"
)

type SabyIdentity struct {
	ID      string
	Code    string
	Aliases []string
}

// SabyMetadata is a verified readback snapshot, never an editable publication
// plan. Both direct reads and cached snapshots must pass the same identity guard.
type SabyMetadata struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	UUID     string   `json:"uuid"`
	Category Category `json:"category"`
	Folder   Category `json:"folder"`
}

type SabyMetadataReader interface {
	Configured() bool
	ReadSabyEditorMetadata(context.Context, SabyIdentity) (SabyMetadata, error)
}

func ValidateSabyMetadata(identity SabyIdentity, metadata SabyMetadata) error {
	if identity.ID == "" || metadata.ID != identity.ID || metadata.Code == "" || metadata.UUID == "" || identity.Code != "" && metadata.Code != identity.Code {
		return errors.New("Идентификаторы карточки СБИС не совпали при чтении")
	}
	for _, alias := range identity.Aliases {
		if len(alias) == 36 && strings.Count(alias, "-") == 4 && !strings.EqualFold(alias, metadata.UUID) {
			return errors.New("UUID карточки СБИС не совпал при чтении")
		}
	}
	return nil
}
