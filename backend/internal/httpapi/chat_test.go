package httpapi

import (
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/chat"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatAdminBoundaryBeforeStorage(t *testing.T) {
	for _, role := range []string{"", "customer", "unknown"} {
		deps := testDependencies(catalogStub{}, authStub{user: &auth.User{ID: 1, AdminRole: role}})
		deps.Chat = chat.NewStore(nil)
		router := NewRouter(discardLogger(), deps)
		for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/chat"}, {"GET", "/api/v1/admin/chat/unread"}, {"GET", "/api/v1/admin/chat/1"}, {"GET", "/api/v1/admin/chat/1/photos/1"}, {"POST", "/api/v1/admin/chat/1/messages"}, {"POST", "/api/v1/admin/chat/1/read"}, {"PATCH", "/api/v1/admin/chat/1"}} {
			r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "test"})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s role %q: %d", route.method, route.path, role, w.Code)
			}
		}
	}
}
func TestChatGuestCookieAndCrossOrigin(t *testing.T) {
	deps := testDependencies(catalogStub{}, authStub{})
	deps.Chat = chat.NewStore(nil)
	deps.CookieSecure = true
	deps.SiteURL = "https://ficusin.ru"
	router := NewRouter(discardLogger(), deps)
	r := httptest.NewRequest("POST", "/api/v1/chat/messages", strings.NewReader(`{"clientId":"short","text":"hello"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || len(cookies[0].Value) != 43 {
		t.Fatalf("unsafe cookie %#v", cookies)
	}
	r = httptest.NewRequest("POST", "/api/v1/chat/messages", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.invalid")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin chat mutation accepted")
	}
}
