package httpapi

import (
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"net/http"
)

func privateFileUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, fileassets.ErrUnavailable) {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище файлов временно недоступно. Файл не сохранён; попробуйте позже."})
	return true
}
