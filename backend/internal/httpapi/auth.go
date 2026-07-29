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
		RequestCode(context.Context, string) error
		VerifyCode(context.Context, string, string, auth.Registration, string) (string, time.Time, error)
		Logout(context.Context, string) error
		UserByToken(context.Context, string) (*auth.User, error)
}

type authHandlers struct {
		logger       *slog.Logger
		service      authService
		cookieSecure bool
}

type requestCodeBody struct {
		Phone string `json:"phone"`
}

type verifyCodeBody struct {
		Phone           string `json:"phone"`
		Code            string `json:"code"`
		FullName        string `json:"fullName"`
		LastName        string `json:"lastName"`
		Patronymic      string `json:"patronymic"`
		Email           string `json:"email"`
		DeliveryAddress string `json:"deliveryAddress"`
		AccountType     string `json:"accountType"`
		CompanyName     string `json:"companyName"`
		INN             string `json:"inn"`
		KPP             string `json:"kpp"`
		LegalAddress    string `json:"legalAddress"`
}

func (handlers authHandlers) requestCode(response http.ResponseWriter, request *http.Request) {
		var body requestCodeBody
		if err := decodeJSON(request, &body); err != nil {
					writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
					return
				}

		phone := auth.NormalizeRussianPhone(body.Phone)
		if phone == "" {
					writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте номер телефона"})
					return
				}

		err := handlers.service.RequestCode(request.Context(), phone)
		if errors.Is(err, auth.ErrRequestTooSoon) {
					writeJSON(response, http.StatusTooManyRequests, errorResponse{
									Error: "Код уже отправлен. Подождите немного перед повторным запросом",
								})
					return
				}
		if err != nil {
					handlers.logger.Error("request code failed", "error", err)
					writeJSON(response, http.StatusInternalServerError, errorResponse{
									Error: "Не удалось отправить код. Попробуйте позднее",
								})
					return
				}

		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (handlers authHandlers) verifyCode(response http.ResponseWriter, request *http.Request) {
		var body verifyCodeBody
		if err := decodeJSON(request, &body); err != nil {
					writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
					return
				}

		phone := auth.NormalizeRussianPhone(body.Phone)
		code := strings.TrimSpace(body.Code)
		if phone == "" || code == "" {
					writeJSON(response, http.StatusBadRequest, errorResponse{
									Error: "Введите телефон и код из смс",
								})
					return
				}

		registration, message := validatedRegistration(body, phone)
		if message != "" {
					writeJSON(response, http.StatusBadRequest, errorResponse{Error: message})
					return
				}

		token, expiresAt, err := handlers.service.VerifyCode(
					request.Context(),
					phone,
					code,
					registration,
					request.UserAgent(),
				)
		switch {
				case errors.Is(err, auth.ErrInvalidCode):
					writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Неверный или истёкший код"})
					return
				case errors.Is(err, auth.ErrTooManyAttempts):
					writeJSON(response, http.StatusTooManyRequests, errorResponse{
									Error: "Слишком много попыток. Запросите новый код",
								})
					return
				case errors.Is(err, auth.ErrRegistrationDetailsRequired):
					writeJSON(response, http.StatusUnprocessableEntity, errorResponse{
									Error: "Укажите имя и тип аккаунта, чтобы завершить регистрацию",
								})
					return
				case err != nil:
					handlers.logger.Error("verify code failed", "error", err)
					writeJSON(response, http.StatusInternalServerError, errorResponse{
									Error: "Не удалось подтвердить код. Попробуйте позднее",
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

// validatedRegistration builds a Registration from the optional profile
// details supplied alongside a verify-code request. Every field is
// optional except FullName and AccountType, which the service only
// requires the first time a phone number registers; here we just
// validate whatever was actually provided.
func validatedRegistration(body verifyCodeBody, phone string) (auth.Registration, string) {
		fullName := strings.TrimSpace(body.FullName)
		lastName := strings.TrimSpace(body.LastName)
		patronymic := strings.TrimSpace(body.Patronymic)
		email := strings.ToLower(strings.TrimSpace(body.Email))
		deliveryAddress := strings.TrimSpace(body.DeliveryAddress)
		accountType := strings.TrimSpace(body.AccountType)
		if accountType == "" {
					accountType = "retail"
				}
		companyName := strings.TrimSpace(body.CompanyName)
		inn := digitsOnly.ReplaceAllString(body.INN, "")
		kpp := digitsOnly.ReplaceAllString(body.KPP, "")

		switch {
				case fullName != "" && (len([]rune(fullName)) < 2 || len([]rune(fullName)) > 120):
					return auth.Registration{}, "Проверьте имя"
				case email != "" && (!emailPattern.MatchString(email) || len(email) > 254):
					return auth.Registration{}, "Проверьте электронную почту"
				case accountType != "retail" && accountType != "wholesale":
					return auth.Registration{}, "Некорректный тип аккаунта"
				case companyName != "" && inn != "" && innLengthInvalid(inn):
					return auth.Registration{}, "Проверьте ИНН организации"
				case kpp != "" && len(kpp) != 9:
					return auth.Registration{}, "КПП должен содержать 9 цифр"
				}

		return auth.Registration{
					FullName:        fullName,
					LastName:        lastName,
					Patronymic:      patronymic,
					Phone:           phone,
					Email:           email,
					DeliveryAddress: deliveryAddress,
					AccountType:     accountType,
					CompanyName:     companyName,
					INN:             inn,
					KPP:             kpp,
					LegalAddress:    strings.TrimSpace(body.LegalAddress),
				}, ""
}

func innLengthInvalid(inn string) bool {
		return len(inn) != 10 && len(inn) != 12
}
