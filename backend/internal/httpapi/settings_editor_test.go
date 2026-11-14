package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type editorSettingsStore struct {
	seasonalSettingsStub
	season string
}

func (s *editorSettingsStore) All() map[string]string {
	return map[string]string{"cdek.packaging_season": s.season, "unrelated-setting": "must-not-be-exposed"}
}

func TestEditorSettingsExposeOnlyPackagingSeason(t *testing.T) {
	for _, role := range []string{admin.RoleOwner, admin.RoleManager, admin.RoleAdministrator} {
		for _, season := range []string{"summer", "winter"} {
			t.Run(role+"/"+season, func(t *testing.T) {
				deps := adminDependencies(&adminRepositoryStub{}, role, "editor@example.com")
				deps.Settings = &editorSettingsStore{season: season}
				response := httptest.NewRecorder()
				NewRouter(discardLogger(), deps).ServeHTTP(response, adminRequest(http.MethodGet, "/api/v1/admin/catalog-editor/settings?include=all", ""))
				var result struct {
					Values map[string]string `json:"values"`
				}
				err := json.Unmarshal(response.Body.Bytes(), &result)
				if response.Code != http.StatusOK || err != nil || len(result.Values) != 1 || result.Values["cdek.packaging_season"] != season {
					t.Fatalf("status=%d body=%s err=%v", response.Code, response.Body.String(), err)
				}
			})
		}
	}
}

func TestEditorSettingsKeepGlobalSettingsRestricted(t *testing.T) {
	for _, tc := range []struct{ role, path string }{
		{"", "/api/v1/admin/catalog-editor/settings"},
		{"unknown", "/api/v1/admin/catalog-editor/settings"},
		{admin.RoleManager, "/api/v1/admin/settings"},
		{admin.RoleAdministrator, "/api/v1/admin/settings"},
	} {
		t.Run(tc.role+tc.path, func(t *testing.T) {
			deps := adminDependencies(&adminRepositoryStub{}, tc.role, "user@example.com")
			deps.Settings = &editorSettingsStore{season: "winter"}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), deps).ServeHTTP(response, adminRequest(http.MethodGet, tc.path, ""))
			if response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEditorSettingsRequireSessionAndKnownSeason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		store   settingsService
		session bool
		status  int
	}{
		{"anonymous", &editorSettingsStore{season: "summer"}, false, http.StatusUnauthorized},
		{"unavailable", nil, true, http.StatusServiceUnavailable},
		{"invalid season", &editorSettingsStore{season: "autumn"}, true, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := adminDependencies(&adminRepositoryStub{}, admin.RoleManager, "editor@example.com")
			deps.Settings = tc.store
			request := adminRequest(http.MethodGet, "/api/v1/admin/catalog-editor/settings", "")
			if !tc.session {
				request.Header.Del("Cookie")
			}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), deps).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
