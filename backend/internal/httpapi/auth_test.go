package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

type recordingAuthService struct {
	registration auth.Registration
	registerErr  error
	loginErr     error
	user         *auth.User
}

func (service *recordingAuthService) Register(
	_ context.Context,
	registration auth.Registration,
	_ string,
) (string, time.Time, error) {
	service.registration = registration
	return "session-token", time.Now().Add(time.Hour), service.registerErr
}

func (service *recordingAuthService) Login(
	context.Context,
	string,
	string,
	string,
) (string, time.Time, error) {
	return "session-token", time.Now().Add(time.Hour), service.loginErr
}

func (service *recordingAuthService) Logout(context.Context, string) error {
	return nil
}

func (service *recordingAuthService) UserByToken(
	context.Context,
	string,
) (*auth.User, error) {
	return service.user, nil
}

func TestRegisterNormalizesInputAndSetsCookie(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"fullName": " Александр ",
		"phone": "8 (915) 615-11-00",
		"email": " TEST@EXAMPLE.COM ",
		"password": "Растение123",
		"accountType": "retail",
		"consent": true
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	dependencies := testDependencies(catalogStub{}, service)
	dependencies.CookieSecure = true
	NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if service.registration.Phone != "+79156151100" {
		t.Fatalf("phone = %q", service.registration.Phone)
	}
	if service.registration.Email != "test@example.com" {
		t.Fatalf("email = %q", service.registration.Email)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.CookieName {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("session cookie is not secure: %#v", cookies[0])
	}
}

func TestRegisterRejectsInvalidWholesaleDetails(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"fullName": "Александр",
		"phone": "9156151100",
		"email": "test@example.com",
		"password": "Растение123",
		"accountType": "wholesale",
		"companyName": "Фикусин",
		"inn": "123",
		"consent": true
	}`))
	response := httptest.NewRecorder()

	NewRouter(
		discardLogger(),
		testDependencies(catalogStub{}, &recordingAuthService{}),
	).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestLoginReturnsUnauthorizedForWrongCredentials(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{loginErr: auth.ErrInvalidCredentials}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"identifier":"test@example.com","password":"wrong"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestRegisterReturnsConflictForExistingAccount(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{registerErr: auth.ErrAccountExists}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"fullName": "Александр",
		"phone": "9156151100",
		"email": "test@example.com",
		"password": "Растение123",
		"accountType": "retail",
		"consent": true
	}`))
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestMeReturnsUser(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{user: &auth.User{ID: 42, FullName: "Александр"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":42`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestMeRejectsMissingCookie(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	response := httptest.NewRecorder()
	NewRouter(
		discardLogger(),
		testDependencies(catalogStub{}, &recordingAuthService{}),
	).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestLoginFailureDoesNotLeakInternalError(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{loginErr: errors.New("database unavailable")}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"identifier":"test@example.com","password":"Растение123"}`),
	)
	response := httptest.NewRecorder()
	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatal("internal error leaked to client")
	}
}
