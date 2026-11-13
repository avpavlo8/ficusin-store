package integration

import (
	"encoding/json"
	"errors"
	"strings"
)

// Verified readonly on native card1101 (2026-11-13), matching the official
// WNCCards/Blocks/DescriptionEditor JsonML value passed to RichEditor.
const sabyEditorDescriptionVersion = "2"

func sabyEditorDescription(text string) (json.RawMessage, error) {
	if sabyEditorDescriptionVersion == "" {
		return nil, errors.New("Формат описания СБИС ещё не подтверждён; карточка не отправлена")
	}
	paragraphs := []any{}
	for _, line := range strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n") {
		paragraphs = append(paragraphs, []any{"p", map[string]string{"version": sabyEditorDescriptionVersion}, line})
	}
	return json.Marshal(paragraphs)
}
