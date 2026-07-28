package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

const maximumJSONBodySize = 1 << 20

var (
	emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	digitsOnly   = regexp.MustCompile(`[^0-9]`)
)

type authService interface {
	Register(context.Context, auth.Registration, string) (string, time.Time, error)
	Login(context.Context, string, string, string) (string, time.Time, error)
	Logout(context.Context, string) error
	UserByToken(context.Context, string) (*auth.User, error)
}

type authHandlers struct {
	logger       *slog.Logger
	service      authService
	cookieSecure bool
}

type registrationBody struct {
	FullName     string `json:"fullName"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	AccountType  string `json:"accountType"`
	CompanyName  string `json:"companyName"`
	INN          string `json:"inn"`
	KPP          string `json:"kpp"`
	LegalAddress string `json:"legalAddress"`
	Consent      bool   `json:"consent"`
}

type loginBody struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

func (handlers authHandlers) register(response http.ResponseWriter, request *http.Request) {
	var body registrationBody
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
		return
	}

	registration, message := validatedRegistration(body)
	if message != "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: message})
		return
	}

	token, expiresAt, err := handlers.service.Register(
		request.Context(),
		registration,
		request.UserAgent(),
	)
	if errors.Is(err, auth.ErrAccountExists) {
		writeJSON(response, http.StatusConflict, errorResponse{
			Error: "Аккаунт с таким телефоном или email уже существует",
		})
		return
	}
	if err != nil {
		handlers.logger.Error("registration failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось создать аккаунт. Попробуйте позднее",
		})
		return
	}

	handlers.setSessionCookie(response, token, expiresAt)
	writeJSON(response, http.StatusCreated, map[string]any{
		"ok":          true,
		"accountType": registration.AccountType,
	})
}

func (handlers authHandlers) login(response http.ResponseWriter, request *http.Request) {
	var body loginBody
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
		return
	}

	identifier := strings.TrimSpace(body.Identifier)
	if identifier == "" || body.Password == "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{
			Error: "Введите телефон или email и пароль",
		})
		return
	}

	token, expiresAt, err := handlers.service.Login(
		request.Context(),
		identifier,
		body.Password,
		request.UserAgent(),
	)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeJSON(response, http.StatusUnauthorized, errorResponse{
			Error: "Неверный телефон, email или пароль",
		})
		return
	}
	if err != nil {
		handlers.logger.Error("login failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось войти. Попробуйте позднее",
		})
		return
	}

	handlers.setSessionCookie(response, token, expiresAt)
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (handlers authHandlers) logout(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(auth.CookieName)
	if err == nil {
		if err := handlers.service.Logout(request.Context(), cookie.Value); err != nil {
			handlers.logger.Error("logout failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось выйти. Попробуйте позднее",
			})
			return
		}
	}

	http.SetCookie(response, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   handlers.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (handlers authHandlers) me(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return
	}

	user, err := handlers.service.UserByToken(request.Context(), cookie.Value)
	if err != nil {
		handlers.logger.Error("session lookup failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось загрузить профиль",
		})
		return
	}
	if user == nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (handlers authHandlers) setSessionCookie(
	response http.ResponseWriter,
	token string,
	expiresAt time.Time,
) {
	http.SetCookie(response, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   handlers.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func decodeJSON(request *http.Request, destination any) error {
	return decodeJSONWithLimit(request, destination, maximumJSONBodySize)
}

func decodeJSONWithLimit(request *http.Request, destination any, limit int64) error {
	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return errors.New("JSON body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func validatedRegistration(body registrationBody) (auth.Registration, string) {
	fullName := strings.TrimSpace(body.FullName)
	email := strings.ToLower(strings.TrimSpace(body.Email))
	phone := auth.NormalizeRussianPhone(body.Phone)
	accountType := "retail"
	if body.AccountType == "wholesale" {
		accountType = "wholesale"
	}
	companyName := strings.TrimSpace(body.CompanyName)
	inn := digitsOnly.ReplaceAllString(body.INN, "")
	kpp := digitsOnly.ReplaceAllString(body.KPP, "")

	switch {
	case len([]rune(fullName)) < 2 || len([]rune(fullName)) > 120:
		return auth.Registration{}, "Укажите имя покупателя"
	case phone == "":
		return auth.Registration{}, "Проверьте номер телефона"
	case !emailPattern.MatchString(email) || len(email) > 254:
		return auth.Registration{}, "Проверьте электронную почту"
	case !auth.PasswordIsAcceptable(body.Password):
		return auth.Registration{}, "Пароль должен содержать не менее 10 символов, букву и цифру"
	case !body.Consent:
		return auth.Registration{}, "Необходимо принять условия обработки данных"
	case accountType == "wholesale" &&
		(len([]rune(companyName)) < 2 || innLengthInvalid(inn)):
		return auth.Registration{}, "Для оптового аккаунта укажите организацию и корректный ИНН"
	case kpp != "" && len(kpp) != 9:
		return auth.Registration{}, "КПП должен содержать 9 цифр"
	}

	return auth.Registration{
		FullName:     fullName,
		Phone:        phone,
		Email:        email,
		Password:     body.Password,
		AccountType:  accountType,
		CompanyName:  companyName,
		INN:          inn,
		KPP:          kpp,
		LegalAddress: strings.TrimSpace(body.LegalAddress),
	}, ""
}

func innLengthInvalid(inn string) bool {
	return len(inn) != 10 && len(inn) != 12
}
