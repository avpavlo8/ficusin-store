package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"path/filepath"
)

type productCreationRepository interface {
	ProductCreationOptions(context.Context, admin.Actor) (admin.CreationOptions, error)
	NewProductCreation(context.Context, admin.Actor) (admin.CreationDraft, error)
	ProductCreation(context.Context, admin.Actor, string) (admin.CreationDraft, error)
	SaveProductCreation(context.Context, admin.Actor, string, int, admin.CreationInput) (admin.CreationDraft, error)
	AddProductCreationPhoto(context.Context, admin.Actor, string, string, []byte) (admin.CreationDraft, error)
	DeleteProductCreationPhoto(context.Context, admin.Actor, string, string) (admin.CreationDraft, error)
	FinishProductCreation(context.Context, admin.Actor, string, int) (admin.CreationDraft, error)
	ProductCreationPhoto(context.Context, string) ([]byte, error)
}

func registerProductCreationRoutes(mux *http.ServeMux, h adminHandlers, environment string) {
	handle := func(action string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			_, actor, ok := h.authorize(w, r, admin.PermissionProductsManage)
			if !ok {
				return
			}
			if action == "options" && (environment != "staging" || actor.Role != admin.RoleOwner) {
				writeJSON(w, 200, admin.CreationOptions{Enabled: false})
				return
			}
			if environment != "staging" {
				http.NotFound(w, r)
				return
			}
			repo, ok := h.repository.(productCreationRepository)
			if !ok {
				writeJSON(w, 503, errorResponse{Error: "Мастер создания пока недоступен"})
				return
			}
			if actor.Role != admin.RoleOwner {
				writeJSON(w, 403, errorResponse{Error: "Создание на площадках доступно владельцу"})
				return
			}
			var result any
			var err error
			id := r.PathValue("draftId")
			switch action {
			case "options":
				result, err = repo.ProductCreationOptions(r.Context(), actor)
			case "new":
				result, err = repo.NewProductCreation(r.Context(), actor)
			case "get":
				result, err = repo.ProductCreation(r.Context(), actor, id)
			case "save":
				var body struct {
					Revision int                 `json:"revision"`
					Data     admin.CreationInput `json:"data"`
				}
				r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
				if decodeJSON(r, &body) != nil {
					writeJSON(w, 400, errorResponse{Error: "Проверьте поля черновика"})
					return
				}
				result, err = repo.SaveProductCreation(r.Context(), actor, id, body.Revision, body.Data)
			case "finish":
				var body struct {
					Revision int `json:"revision"`
				}
				if decodeJSON(r, &body) != nil {
					writeJSON(w, 400, errorResponse{Error: "Проверьте версию черновика"})
					return
				}
				result, err = repo.FinishProductCreation(r.Context(), actor, id, body.Revision)
			case "photo":
				r.Body = http.MaxBytesReader(w, r.Body, maxProductPhotoBytes+(1<<20))
				if r.ParseMultipartForm(maxProductPhotoBytes) != nil {
					writeJSON(w, 400, errorResponse{Error: "Фото должно быть не больше 12 МБ"})
					return
				}
				if r.MultipartForm != nil {
					defer r.MultipartForm.RemoveAll()
				}
				file, header, e := r.FormFile("file")
				if e != nil {
					writeJSON(w, 400, errorResponse{Error: "Выберите фотографию"})
					return
				}
				defer file.Close()
				raw, e := io.ReadAll(io.LimitReader(file, maxProductPhotoBytes+1))
				if e != nil || len(raw) == 0 || len(raw) > maxProductPhotoBytes {
					writeJSON(w, 400, errorResponse{Error: "Фото должно быть не больше 12 МБ"})
					return
				}
				kind := http.DetectContentType(raw)
				if kind != "image/jpeg" && kind != "image/png" && kind != "image/gif" {
					writeJSON(w, 400, errorResponse{Error: "Поддерживаются JPEG, PNG и GIF"})
					return
				}
				prepared, e := photos.Prepare(raw, photos.SizeLarge.MaxSide)
				if e != nil {
					writeJSON(w, 400, errorResponse{Error: "Не удалось прочитать фотографию"})
					return
				}
				name := filepath.Base(header.Filename)
				if len(name) > 200 {
					name = "Фотография"
				}
				result, err = repo.AddProductCreationPhoto(r.Context(), actor, id, name, prepared)
			case "deletePhoto":
				result, err = repo.DeleteProductCreationPhoto(r.Context(), actor, id, r.PathValue("photoId"))
			}
			if err != nil {
				switch {
				case errors.Is(err, admin.ErrForbidden):
					writeJSON(w, 403, errorResponse{Error: err.Error()})
				case errors.Is(err, admin.ErrInvalidInput):
					writeJSON(w, 400, errorResponse{Error: err.Error()})
				case errors.Is(err, pgx.ErrNoRows):
					writeJSON(w, 404, errorResponse{Error: "Черновик не найден или уже завершён"})
				default:
					h.failed(w, "product creation", err)
				}
				return
			}
			writeJSON(w, 200, result)
		}
	}
	mux.HandleFunc("GET /api/v1/admin/product-creations/options", handle("options"))
	mux.HandleFunc("POST /api/v1/admin/product-creations", handle("new"))
	mux.HandleFunc("GET /api/v1/admin/product-creations/{draftId}", handle("get"))
	mux.HandleFunc("PUT /api/v1/admin/product-creations/{draftId}", handle("save"))
	mux.HandleFunc("POST /api/v1/admin/product-creations/{draftId}/photos", handle("photo"))
	mux.HandleFunc("DELETE /api/v1/admin/product-creations/{draftId}/photos/{photoId}", handle("deletePhoto"))
	mux.HandleFunc("POST /api/v1/admin/product-creations/{draftId}/finish", handle("finish"))
	mux.HandleFunc("GET /api/v1/product-creation-photos/{photoId}", func(w http.ResponseWriter, r *http.Request) {
		if environment != "staging" {
			http.NotFound(w, r)
			return
		}
		repo, ok := h.repository.(productCreationRepository)
		if !ok {
			http.NotFound(w, r)
			return
		}
		raw, err := repo.ProductCreationPhoto(r.Context(), r.PathValue("photoId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(raw)
	})
}
