package httpapi

import (
	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
	"testing"
)

func TestEditorCategoryNamesResolveLinkedCards(t *testing.T) {
	raw := []byte(`{"result":[{"description_category_id":10,"category_name":"Растения","children":[{"type_id":20,"type_name":"Саженец"}]}]}`)
	if got := editorCategoryName(raw, "ozon", ce.Category{ID: "10", TypeID: 20}); got != "Растения / Саженец" {
		t.Fatal(got)
	}
	raw = []byte(`{"data":[{"subjectID":30,"subjectName":"Растения","parentName":"Сад"}]}`)
	if got := editorCategoryName(raw, "wb", ce.Category{ID: "30"}); got != "Сад / Растения" {
		t.Fatal(got)
	}
}
