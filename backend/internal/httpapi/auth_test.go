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
		phone        string
		code         string
		registration auth.Registration
		requestErr   error
		verifyErr    error
		user         *auth.User
}

func (service *recordingAuthService) RequestCode(_ context.Context, phone string) error {
		service.phone = phone
		return service.requestErr
}

func (service *recordingAuthService) VerifyCode(
		_ context.Context,
		phone, code string,
		registration auth.Registration,
		_ string,
	) (string, time.Time, error) {
		service.phone = phone
		service.code = code
		service.registration = registration
		return "session-token", time.Now().Add(time.Hour), service.verifyErr
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

func TestVerifyCodeNormalizesInputAndSetsCookie(t *testing.T) {
		t.Parallel()

		service := &recordingAuthService{}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-code", strings.NewReader(`{
				"phone": "8 (915) 615-11-00",
						"code": "1234",
								"fullName": " Александр ",
										"email": " TEST@EXAMPLE.COM ",
												"accountType": "retail"
													}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()

		dependencies := testDependencies(catalogStub{}, service)
		dependencies.CookieSecure = true
		NewRouter(discardLogger(), dependencies).ServeHTTP(response, request)

		if response.Code != http.StatusOK {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body)
				}
		if service.phone != "+79156151100" {
					t.Fatalf("phone = %q", service.phone)
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

func TestVerifyCodeRejectsInvalidWholesaleDetails(t *testing.T) {
		t.Parallel()

		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-code", strings.NewReader(`{
				"phone": "9156151100",
						"code": "1234",
								"fullName": "Александр",
										"accountType": "wholesale",
												"companyName": "Фикусин",
														"inn": "123"
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

func TestVerifyCodeReturnsUnauthorizedForInvalidCode(t *testing.T) {
		t.Parallel()

		service := &recordingAuthService{verifyErr: auth.ErrInvalidCode}
		request := httptest.NewRequest(
					http.MethodPost,
					"/api/v1/auth/verify-code",
					strings.NewReader(`{"phone":"9156151100","code":"0000"}`),
				)
		response := httptest.NewRecorder()

		NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
			ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body)
				}
}

func TestVerifyCodeReturnsUnprocessableEntityWhenDetailsMissing(t *testing.T) {
		t.Parallel()

		service := &recordingAuthService{verifyErr: auth.ErrRegistrationDetailsRequired}
		request := httptest.NewRequest(
					http.MethodPost,
					"/api/v1/auth/verify-code",
					strings.NewReader(`{"phone":"9156151100","code":"1234"}`),
				)
		response := httptest.NewRecorder()

		NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
			ServeHTTP(response, request)

		if response.Code != http.StatusUnprocessableEntity {
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

func TestVerifyCodeFailureDoesNotLeakInternalError(t *testing.T) {
		t.Parallel()

		service := &recordingAuthService{verifyErr: errors.New("database unavailable")}
		request := httptest.NewRequest(
					http.MethodPost,
					"/api/v1/auth/verify-code",
					strings.NewReader(`{"phone":"9156151100","code":"1234","fullName":"Александр","accountType":"retail"}`),
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
