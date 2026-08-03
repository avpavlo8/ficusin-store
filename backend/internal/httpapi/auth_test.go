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
	checkID      string
	registration auth.Registration
	profile      auth.Profile
	requestErr   error
	confirmErr   error
	profileErr   error
	pending      bool
	user         *auth.User
}

func (service *recordingAuthService) UpdateProfile(
	_ context.Context,
	_ int64,
	profile auth.Profile,
) error {
	service.profile = profile
	return service.profileErr
}

func (service *recordingAuthService) SaveAvatar(context.Context, int64, []byte, string) error {
	return nil
}

func (service *recordingAuthService) DeleteAvatar(context.Context, int64) error {
	return nil
}

func (service *recordingAuthService) Avatar(context.Context, int64) ([]byte, string, error) {
	return nil, "", nil
}

func (service *recordingAuthService) RequestCall(_ context.Context, phone string) (string, string, string, error) {
	service.phone = phone
	return "check-id", "78000000000", "+7 (800) 000-00-00", service.requestErr
}

func (service *recordingAuthService) ConfirmCall(
	_ context.Context,
	phone, checkID string,
	registration auth.Registration,
	_ auth.ClientMeta,
) (string, time.Time, bool, error) {
	service.phone = phone
	service.checkID = checkID
	service.registration = registration
	if service.confirmErr != nil || service.pending {
		return "", time.Time{}, service.pending, service.confirmErr
	}
	return "session-token", time.Now().Add(time.Hour), false, nil
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

func TestVerifyCallNormalizesInputAndSetsCookie(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-code", strings.NewReader(`{
		"phone": "8 (915) 615-11-00",
		"checkId": "check-id",
		"fullName": " Александр ",
		"email": " TEST@EXAMPLE.COM ",
		"accountType": "retail",
		"consent": true
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

func TestVerifyCallRejectsInvalidWholesaleDetails(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-code", strings.NewReader(`{
		"phone": "9156151100",
		"checkId": "check-id",
		"fullName": "Александр",
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

func TestVerifyCallReturnsAcceptedWhilePending(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{pending: true}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"check-id"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestVerifyCallReturnsUnauthorizedForInvalidCheck(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{confirmErr: auth.ErrInvalidCode}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"stale-check"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestVerifyCallReturnsUnprocessableEntityWhenDetailsMissing(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{confirmErr: auth.ErrRegistrationDetailsRequired}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"check-id"}`),
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

// The role travels with the account as stored, and is never inferred from
// the email address on the profile.
func TestMeReturnsStoredRole(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{user: &auth.User{
		ID: 42, Email: "Owner@Example.com", FullName: "Александр", AdminRole: "owner",
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"adminRole":"owner"`) {
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

func TestVerifyCallFailureDoesNotLeakInternalError(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{confirmErr: errors.New("database unavailable")}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"check-id","fullName":"Александр","accountType":"retail","consent":true}`),
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

func TestVerifyCallKeepsLoginSeparateFromRegistration(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"check-id","flow":"login"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if service.registration.Flow != "login" {
		t.Fatalf("flow = %q, want login", service.registration.Flow)
	}
}

func TestVerifyCallReturnsNotFoundForUnknownLogin(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{confirmErr: auth.ErrAccountNotFound}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{"phone":"9156151100","checkId":"check-id","flow":"login"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestVerifyCallReturnsConflictForDuplicateRegistration(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{confirmErr: auth.ErrAccountExists}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/verify-code",
		strings.NewReader(`{
			"phone":"9156151100",
			"checkId":"check-id",
			"flow":"register",
			"fullName":"Александр",
			"accountType":"retail",
			"consent":true
		}`),
	)
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestUpdateProfileNormalizesAndSaves(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{user: &auth.User{ID: 42, FullName: "Старое"}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/account/profile", strings.NewReader(`{
		"fullName": " Александр ",
		"lastName": " Павлов ",
		"email": " TEST@EXAMPLE.COM ",
		"deliveryAddress": " Рязань, ул. Ленина 1 "
	}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if service.profile.FullName != "Александр" || service.profile.LastName != "Павлов" {
		t.Fatalf("name = %q %q", service.profile.FullName, service.profile.LastName)
	}
	if service.profile.Email != "test@example.com" {
		t.Fatalf("email = %q", service.profile.Email)
	}
	if service.profile.DeliveryAddress != "Рязань, ул. Ленина 1" {
		t.Fatalf("deliveryAddress = %q", service.profile.DeliveryAddress)
	}
}

func TestUpdateProfileRejectsMissingName(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{user: &auth.User{ID: 42}}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/account/profile",
		strings.NewReader(`{"fullName":" "}`),
	)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestUpdateProfileRejectsInvalidEmail(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{user: &auth.User{ID: 42}}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/account/profile",
		strings.NewReader(`{"fullName":"Александр","email":"not-an-email"}`),
	)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestUpdateProfileRequiresSession(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/account/profile",
		strings.NewReader(`{"fullName":"Александр"}`),
	)
	response := httptest.NewRecorder()

	NewRouter(
		discardLogger(),
		testDependencies(catalogStub{}, &recordingAuthService{}),
	).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestUpdateProfileReportsDuplicateEmail(t *testing.T) {
	t.Parallel()

	service := &recordingAuthService{
		user:       &auth.User{ID: 42},
		profileErr: auth.ErrEmailTaken,
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/account/profile",
		strings.NewReader(`{"fullName":"Александр","email":"taken@example.com"}`),
	)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	response := httptest.NewRecorder()

	NewRouter(discardLogger(), testDependencies(catalogStub{}, service)).
		ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}
