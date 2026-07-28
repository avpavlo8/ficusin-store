package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/order"
)

type orderCreator interface {
	Create(context.Context, order.CreateInput) (order.Created, error)
}

type createOrderBody struct {
	Customer struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Email   string `json:"email"`
		Address string `json:"address"`
		Comment string `json:"comment"`
	} `json:"customer"`
	Delivery string `json:"delivery"`
	Items    []struct {
		ID       string `json:"id"`
		Quantity int    `json:"quantity"`
	} `json:"items"`
	CDEK struct {
		CityCode   int    `json:"cityCode"`
		CityName   string `json:"cityName"`
		OfficeCode string `json:"officeCode"`
	} `json:"cdek"`
}

func createOrderHandler(
	logger *slog.Logger,
	authentication authService,
	creator orderCreator,
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body createOrderBody
		if err := decodeJSON(request, &body); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные заказа"})
			return
		}
		name := strings.TrimSpace(body.Customer.Name)
		phone := auth.NormalizeRussianPhone(body.Customer.Phone)
		email := strings.ToLower(strings.TrimSpace(body.Customer.Email))
		address := strings.TrimSpace(body.Customer.Address)
		delivery := strings.TrimSpace(body.Delivery)
		switch {
		case name == "" || email == "":
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Заполните имя, телефон и email"})
			return
		case phone == "":
			writeJSON(response, http.StatusBadRequest, errorResponse{
				Error: "Введите корректный российский номер телефона",
			})
			return
		case !emailPattern.MatchString(email) || len(email) > 254:
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Проверьте электронную почту"})
			return
		case delivery != "pickup" && delivery != "cdek" && address == "":
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите адрес или пункт выдачи"})
			return
		}

		var customerID *int64
		if cookie, err := request.Cookie(auth.CookieName); err == nil {
			user, lookupErr := authentication.UserByToken(request.Context(), cookie.Value)
			if lookupErr != nil {
				logger.Error("order session lookup failed", "error", lookupErr)
			} else if user != nil {
				customerID = &user.ID
			}
		}
		items := make([]order.ItemInput, 0, len(body.Items))
		for _, item := range body.Items {
			items = append(items, order.ItemInput{
				ID: strings.TrimSpace(item.ID), Quantity: item.Quantity,
			})
		}
		created, err := creator.Create(request.Context(), order.CreateInput{
			Customer: order.CustomerInput{
				Name: name, Phone: phone, Email: email, Address: address,
				Comment: strings.TrimSpace(body.Customer.Comment),
			},
			Delivery: delivery,
			Items:    items,
			CDEK: order.CDEKInput{
				CityCode: body.CDEK.CityCode, CityName: strings.TrimSpace(body.CDEK.CityName),
				OfficeCode: strings.TrimSpace(body.CDEK.OfficeCode),
			},
			CustomerID: customerID,
		})
		var validationError *order.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: validationError.Message})
			return
		}
		if err != nil {
			logger.Error("create order failed", "error", err)
			writeJSON(response, http.StatusInternalServerError, errorResponse{
				Error: "Не удалось сохранить заказ",
			})
			return
		}
		writeJSON(response, http.StatusCreated, created)
	})
}
