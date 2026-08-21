package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5"
)

type variantMediaRepository interface {
	ListVariantMedia(context.Context, int64) ([]admin.ProductMedia, error)
	AddUploadedVariantMedia(context.Context, admin.Actor, int64, string, string, string) (admin.ProductMedia, error)
	DeleteVariantMedia(context.Context, admin.Actor, int64, int64) error
	SetPrimaryVariantMedia(context.Context, admin.Actor, int64, int64) error
}

func listVariantMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok { return }
		variantID, ok := pathID(response, request)
		if !ok { return }
		provider, ok := adminAPI.repository.(variantMediaRepository)
		if !ok { adminAPI.failed(response, "variant media unavailable", errors.New("variant media unavailable")); return }
		items, err := provider.ListVariantMedia(request.Context(), variantID)
		if err != nil { adminAPI.failed(response, "list variant media", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"media": items})
	}
}

func uploadVariantMediaHandler(adminAPI adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		variantID, ok := pathID(response, request)
		if !ok { return }
		if storage == nil || !storage.Configured() {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище фотографий не настроено"})
			return
		}
		provider, ok := adminAPI.repository.(variantMediaRepository)
		if !ok { adminAPI.failed(response, "variant media unavailable", errors.New("variant media unavailable")); return }
		request.Body = http.MaxBytesReader(response, request.Body, maxProductPhotoBytes+1<<20)
		if err := request.ParseMultipartForm(maxProductPhotoBytes); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Файл слишком большой или повреждён"})
			return
		}
		file, _, err := request.FormFile("file")
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите фотографию"}); return }
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, maxProductPhotoBytes+1))
		if err != nil || len(raw) == 0 || len(raw) > maxProductPhotoBytes {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Фотография должна быть не больше 12 МБ"})
			return
		}
		contentType := http.DetectContentType(raw)
		if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Поддерживаются JPEG, PNG и GIF"})
			return
		}
		large, err := photos.Prepare(raw, photos.SizeLarge.MaxSide)
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать фотографию"}); return }
		card, err := photos.Prepare(raw, photos.SizeCard.MaxSide)
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось подготовить фотографию"}); return }
		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil { adminAPI.failed(response, "create variant media token", err); return }
		token := hex.EncodeToString(tokenBytes)
		prefix := fmt.Sprintf("variants/%d/%s", variantID, token)
		cardKey, largeKey := prefix+"-card.jpg", prefix+"-large.jpg"
		if err := storage.Put(request.Context(), cardKey, card, "image/jpeg"); err != nil { adminAPI.failed(response, "upload variant card image", err); return }
		if err := storage.Put(request.Context(), largeKey, large, "image/jpeg"); err != nil { adminAPI.failed(response, "upload variant large image", err); return }
		item, err := provider.AddUploadedVariantMedia(request.Context(), actor, variantID, "upload://variant/"+token, storage.PublicURL(cardKey), storage.PublicURL(largeKey))
		if err != nil { adminAPI.failed(response, "save variant media", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"media": item})
	}
}

func deleteVariantMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		variantID, ok := pathID(response, request)
		if !ok { return }
		mediaID, err := strconv.ParseInt(strings.TrimSpace(request.PathValue("mediaId")), 10, 64)
		if err != nil || mediaID < 1 { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная фотография"}); return }
		provider, ok := adminAPI.repository.(variantMediaRepository)
		if !ok { adminAPI.failed(response, "variant media unavailable", errors.New("variant media unavailable")); return }
		if err := provider.DeleteVariantMedia(request.Context(), actor, variantID, mediaID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) { writeJSON(response, http.StatusNotFound, errorResponse{Error: "Фотография не найдена"}); return }
			adminAPI.failed(response, "delete variant media", err); return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"deleted": true})
	}
}

func primaryVariantMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		variantID, ok := pathID(response, request)
		if !ok { return }
		mediaID, err := strconv.ParseInt(strings.TrimSpace(request.PathValue("mediaId")), 10, 64)
		if err != nil || mediaID < 1 { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная фотография"}); return }
		provider, ok := adminAPI.repository.(variantMediaRepository)
		if !ok { adminAPI.failed(response, "variant media unavailable", errors.New("variant media unavailable")); return }
		if err := provider.SetPrimaryVariantMedia(request.Context(), actor, variantID, mediaID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) { writeJSON(response, http.StatusNotFound, errorResponse{Error: "Фотография не найдена"}); return }
			adminAPI.failed(response, "set primary variant media", err); return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	}
}
