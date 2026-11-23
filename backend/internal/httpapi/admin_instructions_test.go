package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

func TestCDEKOfficeCredentialsRequireStaffAndAreNotCacheable(t *testing.T) {
	const path = "/api/v1/admin/instructions/cdek-credentials"
	credentials := CDEKOfficeCredentials{Login: "fixture-login", Password: "fixture-password"}
	cases := []struct {
		name        string
		role        string
		session     bool
		credentials CDEKOfficeCredentials
		status      int
	}{
		{"guest", "", false, credentials, http.StatusUnauthorized},
		{"customer", "", true, credentials, http.StatusForbidden},
		{"manager", admin.RoleManager, true, credentials, http.StatusOK},
		{"administrator", admin.RoleAdministrator, true, credentials, http.StatusOK},
		{"owner", admin.RoleOwner, true, credentials, http.StatusOK},
		{"incomplete", admin.RoleOwner, true, CDEKOfficeCredentials{Login: credentials.Login}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := adminDependencies(&adminRepositoryStub{}, tc.role, "")
			deps.CDEKOffice = tc.credentials
			request := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.session {
				request = adminRequest(http.MethodGet, path, "")
			}
			response := httptest.NewRecorder()
			NewRouter(discardLogger(), deps).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("response must not be stored in a cache")
			}
			if tc.status == http.StatusOK {
				var got CDEKOfficeCredentials
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got != credentials {
					t.Fatal("staff received incorrect cabinet credentials")
				}
				return
			}
			if strings.Contains(response.Body.String(), credentials.Login) || strings.Contains(response.Body.String(), credentials.Password) {
				t.Fatal("credentials included in error response")
			}
		})
	}
}
