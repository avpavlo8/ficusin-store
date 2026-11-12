package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMockSabyEditorReferencesEnvironmentBoundary(t *testing.T) {
	for _, mode := range []string{"mock", "live", "disabled", ""} {
		for _, environment := range []string{"staging", "development", "production", "", "test"} {
			t.Run(mode+"/"+environment, func(t *testing.T) {
				refs, ok := mockSabyEditorReferences(mode, environment)
				want := mode == "mock" && (environment == "staging" || environment == "development")
				if ok != want || (refs != nil) != want {
					t.Fatalf("mode=%q environment=%q: got refs=%v ok=%v, want enabled=%v", mode, environment, refs, ok, want)
				}
			})
		}
	}
}

func TestSabyEditorReferencesMockNeedsNoDatabaseOrLiveCache(t *testing.T) {
	for _, environment := range []string{"staging", "development"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("APP_ENV", environment)
			// A nil pool proves preview references neither need a synchronised
			// Saby snapshot nor read/write any live or mock reference cache.
			repo := NewPostgresRepository(nil).WithEditorPublicationMode("mock")
			refs, err := repo.SabyEditorReferences(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(refs)
			if err != nil {
				t.Fatal(err)
			}
			// Match the picker API contract, not just internal Go map types.
			var result struct {
				Categories []struct{ ID, Name string }
				Folders    []struct{ ID, Name string }
			}
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Categories) == 0 || len(result.Folders) == 0 {
				t.Fatalf("empty picker data: %s", encoded)
			}
			seen := make(map[string]bool)
			for _, list := range [][]struct{ ID, Name string }{result.Categories, result.Folders} {
				for _, item := range list {
					if !strings.HasPrefix(item.ID, "mock-saby-") || !strings.Contains(item.Name, "Тест СБИС") || seen[item.ID] {
						t.Fatalf("reference must be distinct and explicitly synthetic: %+v", item)
					}
					seen[item.ID] = true
				}
			}
			// Each request owns its maps: a caller cannot poison later data.
			refs["folders"].([]map[string]string)[0]["id"] = "changed"
			again, err := repo.SabyEditorReferences(context.Background())
			if err != nil || again["folders"].([]map[string]string)[0]["id"] != "mock-saby-folder-plants" {
				t.Fatalf("references leaked caller mutation: %v, %v", again, err)
			}
		})
	}
}
