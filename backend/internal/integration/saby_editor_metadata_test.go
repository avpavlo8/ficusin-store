package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

func TestSabyEditorMetadataUsesTypedSingletonHierarchyIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		typeName string
		entity   string
		want     string
		bad      bool
	}{
		{"observed folder", []any{float64(3333)}, "Идентификатор", "Иерархия", "3333", false},
		{"observed category", []any{float64(1041)}, "Идентификатор", "Иерархия", "1041", false},
		{"unassigned", nil, "Идентификатор", "Иерархия", "", false},
		{"empty", []any{}, "Идентификатор", "Иерархия", "", false},
		{"ambiguous", []any{float64(1041), float64(3333)}, "Идентификатор", "Иерархия", "", true},
		{"wrong entity", []any{float64(1041)}, "Идентификатор", "Номенклатура", "", true},
		{"wrong type", []any{float64(1041)}, "Строка", "Иерархия", "", true},
		{"untyped scalar", float64(1041), "Идентификатор", "Иерархия", "", true},
		{"negative", []any{float64(-1)}, "Идентификатор", "Иерархия", "", true},
		{"fraction", []any{1041.5}, "Идентификатор", "Иерархия", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := map[string]any{"s": []any{map[string]any{"n": "Категория", "t": tc.typeName, "s": tc.entity}}, "d": []any{tc.value}}
			got, err := sabyEditorHierarchyID(record, "Категория")
			if got != tc.want || (err != nil) != tc.bad {
				t.Fatalf("got %q/%v, want %q/error=%v", got, err, tc.want, tc.bad)
			}
		})
	}
}

func TestSabyEditorMetadataDeadlineIncludesSessionLock(t *testing.T) {
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.mu.Lock()
	defer client.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	source := &SabyEditorMetadataSource{Client: client}
	_, err := source.ReadSabyEditorMetadata(ctx, catalogeditor.SabyIdentity{ID: "1101", Code: "X269652448"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("metadata exceeded its deadline while waiting for the shared session lock: %v", err)
	}
}

func TestSabyEditorMetadataAdapterOnlyReadsAndGuardsIdentityBeforeNames(t *testing.T) {
	uuid := "0f56c2bd-2669-473f-9b94-875d521998a7"
	returnedID := 1101
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			_, _ = io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		if r.URL.Path != "/service/" || r.Method != http.MethodPost {
			t.Errorf("unexpected external operation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Content-Type") != "application/json-rpc;charset=utf-8" {
			t.Error("RPC UTF-8 content type missing")
		}
		var request struct {
			Method string
			Params map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Method != "Номенклатура.Прочитать" || request.Params["ИдО"] != float64(1101) {
			t.Errorf("unexpected RPC: %+v", request)
			w.WriteHeader(400)
			return
		}
		if value, present := request.Params["Расширение"]; !present || value != nil {
			t.Error("required nil extension missing")
		}
		reads++
		record := map[string]any{"_type": "record", "s": []any{
			map[string]any{"n": "@Номенклатура", "t": "Число целое"}, map[string]any{"n": "НомНомер", "t": "Строка"}, map[string]any{"n": "Идентификатор", "t": "UUID"},
			map[string]any{"n": "Категория", "t": "Идентификатор", "s": "Иерархия"}, map[string]any{"n": "Раздел", "t": "Идентификатор", "s": "Иерархия"},
		}, "d": []any{returnedID, "X269652448", uuid, []any{1041}, []any{3333}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": record, "id": 1})
	}))
	defer server.Close()
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.authURL = server.URL + "/auth"
	client.serviceURL = server.URL + "/service/"
	resolver := &sabyHierarchyFixture{}
	source := &SabyEditorMetadataSource{Client: client, Hierarchy: resolver}
	identity := catalogeditor.SabyIdentity{ID: "1101", Code: "X269652448", Aliases: []string{uuid}}
	metadata, err := source.ReadSabyEditorMetadata(context.Background(), identity)
	if err != nil || metadata.Category.ID != "1041" || metadata.Category.Name != "Комнатные растения" || metadata.Folder.ID != "3333" || metadata.Folder.Name != "Цветы маркетплейс" {
		t.Fatalf("unexpected metadata %+v/%v", metadata, err)
	}
	if reads != 1 || resolver.reads != 2 {
		t.Fatalf("unexpected read count %d/%d", reads, resolver.reads)
	}
	returnedID = 1102
	if _, err = source.ReadSabyEditorMetadata(context.Background(), identity); err == nil {
		t.Fatal("foreign nomenclature identity accepted")
	}
	if resolver.reads != 2 {
		t.Fatal("foreign identity caused hierarchy reads")
	}
	identity.ID = "STG-EDITOR-2000000000000"
	if _, err = source.ReadSabyEditorMetadata(context.Background(), identity); err == nil || reads != 2 {
		t.Fatal("noncanonical ID caused remote lookup")
	}
}

func TestSabyEditorCategoryNameUsesVerifiedReadContract(t *testing.T) {
	returnedID := 1041
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			_, _ = io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var request struct {
			Method string
			Params map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		fields, _ := request.Params["AdditionalFields"].([]any)
		option, present := request.Params["Option"]
		if r.Method != http.MethodPost || r.URL.Path != "/service/" || request.Method != "NomenclatureCategory.Read" || request.Params["Id"] != float64(1041) || !present || option != nil || len(fields) != 2 || fields[0] != "Name" || fields[1] != "Identifier" {
			t.Errorf("unverified category read contract: %+v", request)
			w.WriteHeader(400)
			return
		}
		record := sabyRecord(map[string]any{"Id": returnedID, "Name": "Комнатные растения", "Identifier": "f15a2190-0c08-4515-8bd3-0174fbb339cf"})
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": record, "id": 1})
	}))
	defer server.Close()
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.authURL = server.URL + "/auth"
	client.serviceURL = server.URL + "/service/"
	source := &SabyEditorHierarchySource{Client: client}
	category, err := source.ResolveSabyEditorHierarchy(context.Background(), "category", "1041")
	if err != nil || category.ID != "1041" || category.Name != "Комнатные растения" {
		t.Fatalf("category readback failed: %+v/%v", category, err)
	}
	returnedID = 1042
	if _, err = source.ResolveSabyEditorHierarchy(context.Background(), "category", "1041"); err == nil {
		t.Fatal("foreign category ID accepted")
	}
}

func TestSabyEditorFolderNameUsesVerifiedReadContract(t *testing.T) {
	returnedID := 3333
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			_, _ = io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var request struct {
			Method string
			Params map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		option, present := request.Params["ИмяМетода"]
		if r.Method != http.MethodPost || r.URL.Path != "/service/" || request.Method != "Nomenclature.Read" || request.Params["ИдО"] != float64(3333) || !present || option != nil {
			t.Errorf("unverified folder read contract: %+v", request)
			w.WriteHeader(400)
			return
		}
		record := sabyRecord(map[string]any{"@Номенклатура": returnedID, "Наименование": "Цветы маркетплейс"})
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": record, "id": 1})
	}))
	defer server.Close()
	client := NewSabyClient("id", "secret", "key", 278, 6)
	client.authURL = server.URL + "/auth"
	client.serviceURL = server.URL + "/service/"
	source := &SabyEditorHierarchySource{Client: client}
	folder, err := source.ResolveSabyEditorHierarchy(context.Background(), "folder", "3333")
	if err != nil || folder.ID != "3333" || folder.Name != "Цветы маркетплейс" {
		t.Fatalf("folder readback failed: %+v/%v", folder, err)
	}
	returnedID = 3334
	if _, err = source.ResolveSabyEditorHierarchy(context.Background(), "folder", "3333"); err == nil {
		t.Fatal("foreign folder ID accepted")
	}
}

type sabyHierarchyFixture struct{ reads int }

func (f *sabyHierarchyFixture) ResolveSabyEditorHierarchy(_ context.Context, kind, id string) (catalogeditor.Category, error) {
	f.reads++
	name := "Комнатные растения"
	if kind == "folder" {
		name = "Цветы маркетплейс"
	}
	return catalogeditor.Category{ID: id, Name: name}, nil
}
