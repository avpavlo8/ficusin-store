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
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5"
)

func generateAICoverHandler(handlers adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	var generationMu sync.Mutex
	inProgress := map[int64]bool{}
	return func(response http.ResponseWriter, request *http.Request) {
		_ = http.NewResponseController(response).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		_, _, ok := handlers.authorize(response, request, admin.PermissionProductsEdit)
		if !ok { return }
		if handlers.ai == nil || !handlers.ai.Configured() || storage == nil || !storage.Configured() {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "AI или хранилище изображений не настроены"})
			return
		}
		productID, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
		if err != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный товар"}); return }
		products, err := handlers.repository.ListProducts(request.Context())
		if err != nil { handlers.failed(response, "read product for ai cover", err); return }
		isPlant := false
		for _, product := range products { if product.ID == productID { isPlant, err = adminProductIsPlant(request.Context(), handlers.repository, product); if err != nil { handlers.failed(response, "resolve product category", err); return }; break } }
		if !isPlant {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "AI-обложка доступна только для растений"})
			return
		}
		generationMu.Lock()
		if inProgress[productID] { generationMu.Unlock(); writeJSON(response, http.StatusConflict, errorResponse{Error: "Обложка для этого товара уже создаётся"}); return }
		inProgress[productID] = true
		generationMu.Unlock()
		defer func() { generationMu.Lock(); delete(inProgress, productID); generationMu.Unlock() }()
		var body struct{ Prompt string `json:"prompt"` }
		if decodeJSON(request, &body) != nil || strings.TrimSpace(body.Prompt) == "" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Пустой промпт"})
			return
		}
		raw, _, err := handlers.ai.GenerateCover(request.Context(), body.Prompt)
		if err != nil { handlers.failedAI(response, "generate ai cover", err); return }
		card, large, err := prepareProductCopies(raw)
		if err != nil { handlers.failedAI(response, "prepare ai cover", err); return }
		token := fmt.Sprint(time.Now().UnixNano())
		prefix := fmt.Sprintf("products/%d/ai-cover-%s", productID, token)
		cardKey, largeKey := prefix+"-card.jpg", prefix+"-large.jpg"
		if err := storage.Put(request.Context(), cardKey, card, "image/jpeg"); err != nil { handlers.failed(response, "store ai card cover", err); return }
		if err := storage.Put(request.Context(), largeKey, large, "image/jpeg"); err != nil { handlers.failed(response, "store ai large cover", err); return }
		// Generation is a proposal only. It is deliberately not linked to the
		// product gallery and cannot become primary until the editor accepts the
		// image diff and explicitly saves the card.
		writeJSON(response, http.StatusCreated, map[string]any{"url": storage.PublicURL(largeKey), "cardUrl": storage.PublicURL(cardKey)})
	}
}

func adminProductIsPlant(ctx context.Context, repository adminRepository, product admin.Product) (bool, error) {
	if product.CategoryID == nil { return false, nil }
	categories, err := repository.ListCategories(ctx)
	if err != nil { return false, err }
	byID := make(map[int64]admin.Category, len(categories))
	for _, category := range categories { byID[category.ID] = category }
	category, exists := byID[*product.CategoryID]
	visited := map[int64]bool{}
	for exists && category.ParentID != nil && !visited[category.ID] { visited[category.ID] = true; category, exists = byID[*category.ParentID] }
	return exists && category.Slug == "plants", nil
}

func prepareProductCopies(raw []byte) (card, large []byte, err error) {
	large, err = photos.Prepare(raw, photos.SizeLarge.MaxSide)
	if err != nil { return nil, nil, err }
	card, err = photos.Prepare(raw, photos.SizeCard.MaxSide)
	if err != nil { return nil, nil, err }
	return card, large, nil
}

// productPhotoStorage is deliberately the tiny slice the HTTP layer needs.
// S3 credentials remain server-side; the browser only sends an image file.
type productPhotoStorage interface {
	Configured() bool
	Put(context.Context, string, []byte, string) error
	PublicURL(string) string
}

type productMediaRepository interface {
	ListProductMedia(context.Context, int64) ([]admin.ProductMedia, error)
	AddUploadedProductMedia(context.Context, admin.Actor, int64, string, string, string) (admin.ProductMedia, error)
	DeleteProductMedia(context.Context, admin.Actor, int64, int64) error
	SetPrimaryProductMedia(context.Context, admin.Actor, int64, int64) error
}

func listProductMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}
		provider, ok := adminAPI.repository.(productMediaRepository)
		if !ok {
			adminAPI.failed(response, "product media unavailable", errors.New("product media unavailable"))
			return
		}
		items, err := provider.ListProductMedia(request.Context(), productID)
		if err != nil {
			adminAPI.failed(response, "list product media", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"media": items})
	}
}

const maxProductPhotoBytes = 12 << 20

func uploadProductMediaHandler(adminAPI adminHandlers, storage productPhotoStorage) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}
		if storage == nil || !storage.Configured() {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Хранилище фотографий не настроено"})
			return
		}
		provider, ok := adminAPI.repository.(productMediaRepository)
		if !ok {
			adminAPI.failed(response, "product media unavailable", errors.New("product media unavailable"))
			return
		}

		request.Body = http.MaxBytesReader(response, request.Body, maxProductPhotoBytes+1<<20)
		if err := request.ParseMultipartForm(maxProductPhotoBytes); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Файл слишком большой или повреждён"})
			return
		}
		file, _, err := request.FormFile("file")
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите фотографию"})
			return
		}
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

		card, large, err := prepareProductCopies(raw)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать фотографию"})
			return
		}
		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil {
			adminAPI.failed(response, "create product media token", err)
			return
		}
		token := hex.EncodeToString(tokenBytes)
		prefix := fmt.Sprintf("products/%d/%s", productID, token)
		cardKey := prefix + "-card.jpg"
		largeKey := prefix + "-large.jpg"
		if err := storage.Put(request.Context(), cardKey, card, "image/jpeg"); err != nil {
			adminAPI.failed(response, "upload product card image", err)
			return
		}
		if err := storage.Put(request.Context(), largeKey, large, "image/jpeg"); err != nil {
			adminAPI.failed(response, "upload product large image", err)
			return
		}
		item, err := provider.AddUploadedProductMedia(
			request.Context(), actor, productID, "upload://"+token,
			storage.PublicURL(cardKey), storage.PublicURL(largeKey),
		)
		if err != nil {
			adminAPI.failed(response, "save product media", err)
			return
		}
		writeJSON(response, http.StatusCreated, map[string]any{"media": item})
	}
}

func deleteProductMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}
		mediaID, err := strconv.ParseInt(strings.TrimSpace(request.PathValue("mediaId")), 10, 64)
		if err != nil || mediaID < 1 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная фотография"})
			return
		}
		provider, ok := adminAPI.repository.(productMediaRepository)
		if !ok {
			adminAPI.failed(response, "product media unavailable", errors.New("product media unavailable"))
			return
		}
		if err := provider.DeleteProductMedia(request.Context(), actor, productID, mediaID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(response, http.StatusNotFound, errorResponse{Error: "Фотография не найдена"})
				return
			}
			adminAPI.failed(response, "delete product media", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"deleted": true})
	}
}

func primaryProductMediaHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}
		mediaID, err := strconv.ParseInt(strings.TrimSpace(request.PathValue("mediaId")), 10, 64)
		if err != nil || mediaID < 1 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная фотография"})
			return
		}
		provider, ok := adminAPI.repository.(productMediaRepository)
		if !ok {
			adminAPI.failed(response, "product media unavailable", errors.New("product media unavailable"))
			return
		}
		if err := provider.SetPrimaryProductMedia(request.Context(), actor, productID, mediaID); err != nil {
			adminAPI.failed(response, "set primary product media", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	}
}
