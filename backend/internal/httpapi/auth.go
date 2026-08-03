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
	RequestCall(ctx context.Context, phone string) (checkID, callPhone, callPhonePretty string, err error)
	ConfirmCall(
		ctx context.Context,
		phone, checkID string,
		registration auth.Registration,
		meta auth.ClientMeta,
	) (token string, expiresAt time.Time, pending bool, err error)
	UpdateProfile(ctx context.Context, customerID int64, profile auth.Profile) error
	SaveAvatar(ctx context.Context, customerID int64, image []byte, mime string) error
	DeleteAvatar(ctx context.Context, customerID int64) error
	Avatar(ctx context.Context, customerID int64) ([]byte, string, error)
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
	Flow  string `json:"flow"`
}

type verifyCodeBody struct {
	Phone           string `json:"phone"`
	Flow            string `json:"flow"`
	CheckID         string `json:"checkId"`
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
	// Consent mirrors the checkbox on the registration form. It matters
	// only when registering; a returning customer agreed once already.
	Consent bool `json:"consent"`
}

func (handlers authHandlers) requestCode(response http.ResponseWriter, request *http.Request) {
	var body requestCodeBody
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
		return
	}

	phone := auth.NormalizeRussianPhone(body.Phone)
	flow := strings.TrimSpace(body.Flow)
	if flow == "" {
		flow = "login"
	}
	if phone == "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте номер телефона"})
		return
	}
	if flow != "login" && flow != "register" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный сценарий авторизации"})
		return
	}

	checkID, callPhone, callPhonePretty, err := handlers.service.RequestCall(request.Context(), phone)
	if errors.Is(err, auth.ErrRequestTooSoon) {
		writeJSON(response, http.StatusTooManyRequests, errorResponse{
			Error: "Номер для звонка уже выдан. Подождите немного перед повторным запросом",
		})
		return
	}
	if err != nil {
		handlers.logger.Error("request call check failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось подготовить звонок. Попробуйте позднее",
		})
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"ok":              true,
		"checkId":         checkID,
		"callPhone":       callPhone,
		"callPhonePretty": callPhonePretty,
	})
}

func (handlers authHandlers) verifyCode(response http.ResponseWriter, request *http.Request) {
	var body verifyCodeBody
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
		return
	}

	phone := auth.NormalizeRussianPhone(body.Phone)
	checkID := strings.TrimSpace(body.CheckID)
	if phone == "" || checkID == "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{
			Error: "Запросите номер для звонка ещё раз",
		})
		return
	}

	registration, message := validatedRegistration(body, phone)
	if message != "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: message})
		return
	}

	token, expiresAt, pending, err := handlers.service.ConfirmCall(
		request.Context(),
		phone,
		checkID,
		registration,
		auth.ClientMeta{UserAgent: request.UserAgent(), IPAddress: clientIP(request)},
	)
	switch {
	case errors.Is(err, auth.ErrConsentRequired):
		writeJSON(response, http.StatusUnprocessableEntity, errorResponse{
			Error: "Подтвердите согласие на обработку персональных данных",
		})
		return
	case errors.Is(err, auth.ErrInvalidCode):
		writeJSON(response, http.StatusUnauthorized, errorResponse{
			Error: "Звонок не подтверждён вовремя. Запросите номер ещё раз",
		})
		return
	case errors.Is(err, auth.ErrAccountNotFound):
		writeJSON(response, http.StatusNotFound, errorResponse{
			Error: "Пользователь с таким номером не найден. Зарегистрируйтесь",
		})
		return
	case errors.Is(err, auth.ErrAccountExists):
		writeJSON(response, http.StatusConflict, errorResponse{
			Error: "Пользователь с таким номером уже зарегистрирован. Войдите",
		})
		return
	case errors.Is(err, auth.ErrRegistrationDetailsRequired):
		writeJSON(response, http.StatusUnprocessableEntity, errorResponse{
			Error: "Заполните обязательные поля регистрации",
		})
		return
	case err != nil:
		handlers.logger.Error("confirm call failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось подтвердить звонок. Попробуйте позднее",
		})
		return
	case pending:
		writeJSON(response, http.StatusAccepted, map[string]bool{"pending": true})
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

type profileBody struct {
	FullName        string `json:"fullName"`
	LastName        string `json:"lastName"`
	Patronymic      string `json:"patronymic"`
	Email           string `json:"email"`
	DeliveryAddress string `json:"deliveryAddress"`
}

// updateProfile saves the details a customer edits in their account page.
// Only the name is required, so someone who signed up with just a phone
// number can fill the rest in whenever they like.
func (handlers authHandlers) updateProfile(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.sessionUser(response, request, "Не удалось сохранить профиль")
	if !ok {
		return
	}

	var body profileBody
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные формы"})
		return
	}

	profile := auth.Profile{
		FullName:        strings.TrimSpace(body.FullName),
		LastName:        strings.TrimSpace(body.LastName),
		Patronymic:      strings.TrimSpace(body.Patronymic),
		Email:           strings.ToLower(strings.TrimSpace(body.Email)),
		DeliveryAddress: strings.TrimSpace(body.DeliveryAddress),
	}
	switch {
	case len([]rune(profile.FullName)) < 2 || len([]rune(profile.FullName)) > 120:
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте имя"})
		return
	case len([]rune(profile.LastName)) > 120 || len([]rune(profile.Patronymic)) > 120:
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте фамилию и отчество"})
		return
	case profile.Email != "" &&
		(!emailPattern.MatchString(profile.Email) || len(profile.Email) > 254):
		writeJSON(response, http.StatusBadRequest, errorResponse{
			Error: "Проверьте электронную почту",
		})
		return
	case len([]rune(profile.DeliveryAddress)) > 500:
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Адрес слишком длинный"})
		return
	}

	err := handlers.service.UpdateProfile(request.Context(), user.ID, profile)
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		writeJSON(response, http.StatusConflict, errorResponse{
			Error: "Эта почта уже привязана к другому аккаунту",
		})
		return
	case errors.Is(err, auth.ErrAccountNotFound):
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return
	case err != nil:
		handlers.logger.Error("update profile failed", "error", err, "customer_id", user.ID)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось сохранить профиль",
		})
		return
	}

	handlers.writeCurrentUser(response, request)
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
	flow := strings.TrimSpace(body.Flow)
	fullName := strings.TrimSpace(body.FullName)
	lastName := strings.TrimSpace(body.LastName)
	patronymic := strings.TrimSpace(body.Patronymic)
	email := strings.ToLower(strings.TrimSpace(body.Email))
	deliveryAddress := strings.TrimSpace(body.DeliveryAddress)
	accountType := strings.TrimSpace(body.AccountType)
	if flow == "" {
		if fullName != "" {
			flow = "register"
		} else {
			flow = "login"
		}
	}
	if accountType == "" {
		accountType = "retail"
	}
	companyName := strings.TrimSpace(body.CompanyName)
	inn := digitsOnly.ReplaceAllString(body.INN, "")
	kpp := digitsOnly.ReplaceAllString(body.KPP, "")

	switch {
	case flow != "login" && flow != "register":
		return auth.Registration{}, "Некорректный сценарий авторизации"
	case flow == "register" && fullName == "":
		return auth.Registration{}, "Укажите имя"
	case flow == "register" && !body.Consent:
		return auth.Registration{}, "Подтвердите согласие на обработку персональных данных"
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
		Flow:            flow,
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
		Consent:         body.Consent,
	}, ""
}

func innLengthInvalid(inn string) bool {
	return len(inn) != 10 && len(inn) != 12
}
