package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5"
)

type collectionCoverRepository interface {
	SetCollectionCover(context.Context, admin.Actor, int64, string) (admin.CollectionDefinition, error)
}

// Collection covers use the same object storage and image hardening path as
// product media. The browser never receives S3 credentials.
func uploadCollectionCoverHandler(adminAPI adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		id, ok := pathID(response, request)
		if !ok { return }
		if storage == nil || !storage.Configured() {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище фотографий не настроено"})
			return
		}
		provider, ok := adminAPI.repository.(collectionCoverRepository)
		if !ok { adminAPI.failed(response, "collection cover unavailable", pgx.ErrNoRows); return }

		request.Body = http.MaxBytesReader(response, request.Body, maxProductPhotoBytes+1<<20)
		if err := request.ParseMultipartForm(maxProductPhotoBytes); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Файл слишком большой или повреждён"})
			return
		}
		file, _, err := request.FormFile("file")
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите изображение"}); return }
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, maxProductPhotoBytes+1))
		if err != nil || len(raw) == 0 || len(raw) > maxProductPhotoBytes {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Изображение должно быть не больше 12 МБ"})
			return
		}
		contentType := http.DetectContentType(raw)
		if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Поддерживаются JPEG, PNG и GIF"})
			return
		}
		prepared, err := photos.Prepare(raw, photos.SizeLarge.MaxSide)
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать изображение"}); return }
		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil { adminAPI.failed(response, "create collection cover token", err); return }
		key := "collections/" + hex.EncodeToString(tokenBytes) + ".jpg"
		if err := storage.Put(request.Context(), key, prepared, "image/jpeg"); err != nil { adminAPI.failed(response, "upload collection cover", err); return }
		item, err := provider.SetCollectionCover(request.Context(), actor, id, storage.PublicURL(key))
		if errors.Is(err, pgx.ErrNoRows) { writeJSON(response, http.StatusNotFound, errorResponse{Error: "Подборка не найдена"}); return }
		if err != nil { adminAPI.failed(response, "save collection cover", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"collection": item})
	}
}
