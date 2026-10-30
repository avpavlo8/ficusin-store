package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type seasonalSettingsStub struct{ saved bool }

func (*seasonalSettingsStub) All() map[string]string {
	return map[string]string{"cdek.packaging_season": "summer"}
}
func (s *seasonalSettingsStub) Save(context.Context, map[string]string) error {
	s.saved = true
	return nil
}

func TestSeasonalSettingsValidationAndOwnerAccess(t *testing.T) {
	for _, tc := range []struct {
		role   string
		season string
		status int
		saved  bool
	}{
		{admin.RoleOwner, "winter", http.StatusOK, true},
		{admin.RoleOwner, "summer", http.StatusOK, true},
		{admin.RoleOwner, "autumn", http.StatusBadRequest, false},
		{admin.RoleOwner, "", http.StatusBadRequest, false},
		{admin.RoleManager, "winter", http.StatusForbidden, false},
	} {
		t.Run(string(tc.role)+"/"+tc.season, func(t *testing.T) {
			store := &seasonalSettingsStub{}
			h := settingsHandlers{auth: adminAuthStub{user: &auth.User{AdminRole: tc.role}}, settings: store}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", strings.NewReader(`{"values":{"cdek.packaging_season":"`+tc.season+`"}}`))
			request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "test"})
			response := httptest.NewRecorder()
			h.update(response, request)
			if response.Code != tc.status || store.saved != tc.saved {
				t.Fatalf("status=%d saved=%v body=%s", response.Code, store.saved, response.Body.String())
			}
		})
	}
}
