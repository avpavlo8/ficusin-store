package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5"
)

type collectionCoverRepository interface {
	SetCollectionCover(context.Context, admin.Actor, int64, string) (admin.CollectionDefinition, error)
}

type collectionCreateRepository interface {
	CreateCollectionDefinition(context.Context, admin.Actor, admin.CollectionDefinitionInput) (admin.CollectionDefinition, error)
}

func readCollectionCover(response http.ResponseWriter, request *http.Request, storage productPhotoStorage) (string, bool) {
	coverURL := strings.TrimSpace(request.FormValue("coverUrl"))
	file, _, err := request.FormFile("file")
	if errors.Is(err, http.ErrMissingFile) {
		if coverURL == "" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Добавьте обложку подборки"})
			return "", false
		}
		return coverURL, true
	}
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать обложку"})
		return "", false
	}
	defer file.Close()
	if storage == nil || !storage.Configured() {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище фотографий не настроено"})
		return "", false
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxProductPhotoBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductPhotoBytes {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Изображение должно быть не больше 12 МБ"})
		return "", false
	}
	contentType := http.DetectContentType(raw)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Поддерживаются JPEG, PNG и GIF"})
		return "", false
	}
	prepared, err := photos.Prepare(raw, photos.SizeLarge.MaxSide)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать изображение"})
		return "", false
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeJSON(response, http.StatusInternalServerError, errorResponse{Error: "Не удалось подготовить обложку"})
		return "", false
	}
	key := "collections/" + hex.EncodeToString(tokenBytes) + ".jpg"
	if err := storage.Put(request.Context(), key, prepared, "image/jpeg"); err != nil {
		writeJSON(response, http.StatusBadGateway, errorResponse{Error: "Не удалось загрузить обложку"})
		return "", false
	}
	return storage.PublicURL(key), true
}

// createCollectionWithCoverHandler keeps a draft entirely in the browser and
// creates the database row only after its required cover is ready.
func createCollectionWithCoverHandler(adminAPI adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		provider, ok := adminAPI.repository.(collectionCreateRepository)
		if !ok {
			adminAPI.failed(response, "collection definitions unavailable", errors.New("collection definitions unavailable"))
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maxProductPhotoBytes+1<<20)
		if err := request.ParseMultipartForm(maxProductPhotoBytes); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Форма или файл слишком большие"})
			return
		}
		coverURL, ok := readCollectionCover(response, request, storage)
		if !ok {
			return
		}
		sortOrder, _ := strconv.Atoi(request.FormValue("sortOrder"))
		var rules []admin.CollectionRule
		if raw := request.FormValue("rules"); raw != "" && json.Unmarshal([]byte(raw), &rules) != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные правила подборки"})
			return
		}
		item, err := provider.CreateCollectionDefinition(request.Context(), actor, admin.CollectionDefinitionInput{Slug: request.FormValue("slug"), Title: request.FormValue("title"), Note: request.FormValue("note"), CoverURL: coverURL, SortOrder: sortOrder, Active: request.FormValue("active") == "true", Mode: request.FormValue("mode"), Rules: rules})
		if errors.Is(err, admin.ErrInvalidInput) {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		if err != nil {
			adminAPI.failed(response, "create collection definition", err)
			return
		}
		writeJSON(response, http.StatusCreated, map[string]any{"collection": item})
	}
}

// Collection covers use the same object storage and image hardening path as
// product media. The browser never receives S3 credentials.
func uploadCollectionCoverHandler(adminAPI adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		id, ok := pathID(response, request)
		if !ok {
			return
		}
		if storage == nil || !storage.Configured() {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище фотографий не настроено"})
			return
		}
		provider, ok := adminAPI.repository.(collectionCoverRepository)
		if !ok {
			adminAPI.failed(response, "collection cover unavailable", pgx.ErrNoRows)
			return
		}

		request.Body = http.MaxBytesReader(response, request.Body, maxProductPhotoBytes+1<<20)
		if err := request.ParseMultipartForm(maxProductPhotoBytes); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Файл слишком большой или повреждён"})
			return
		}
		coverURL, ok := readCollectionCover(response, request, storage)
		if !ok {
			return
		}
		item, err := provider.SetCollectionCover(request.Context(), actor, id, coverURL)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(response, http.StatusNotFound, errorResponse{Error: "Подборка не найдена"})
			return
		}
		if err != nil {
			adminAPI.failed(response, "save collection cover", err)
			return
		}
		writeJSON(response, http.StatusCreated, map[string]any{"collection": item})
	}
}
