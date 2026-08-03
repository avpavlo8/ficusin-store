package httpapi

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

// maximumAvatarBytes caps what we are willing to store per customer. The
// browser downscales the picture to roughly 256×256 JPEG before uploading,
// which lands far below this, so hitting the limit means something other
// than our own form posted the request.
const maximumAvatarBytes = 512 * 1024

var allowedAvatarMimes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

type avatarBody struct {
	// Image is a data URL produced by the browser's canvas export.
	Image string `json:"image"`
}

// detectImageMime reads the file's own signature. http.DetectContentType
// recognises the three formats we accept and answers with something
// harmless ("application/octet-stream", "text/plain; charset=utf-8") for
// anything else, which the allow-list then rejects.
func detectImageMime(image []byte) string {
	detected := http.DetectContentType(image)
	if base, _, found := strings.Cut(detected, ";"); found {
		return strings.TrimSpace(base)
	}
	return detected
}

func (handlers authHandlers) uploadAvatar(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.sessionUser(response, request, "Не удалось сохранить фото")
	if !ok {
		return
	}

	var body avatarBody
	if err := decodeJSONWithLimit(request, &body, maximumAvatarBytes*2); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать файл"})
		return
	}

	_, payload, found := strings.Cut(strings.TrimPrefix(body.Image, "data:"), ";base64,")
	if !found {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Неподдерживаемый формат файла"})
		return
	}
	image, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Не удалось прочитать файл"})
		return
	}
	if len(image) == 0 || len(image) > maximumAvatarBytes {
		writeJSON(response, http.StatusRequestEntityTooLarge, errorResponse{
			Error: "Файл слишком большой — выберите изображение поменьше",
		})
		return
	}
	// The type claimed in the data URL is whatever the sender typed, so it
	// decides nothing: the type is read from the bytes themselves. Storing
	// the claimed type would let someone save a script under an image
	// content type and have it served back from our own origin.
	mime := detectImageMime(image)
	if _, allowed := allowedAvatarMimes[mime]; !allowed {
		writeJSON(response, http.StatusUnsupportedMediaType, errorResponse{
			Error: "Подойдёт JPEG, PNG или WebP",
		})
		return
	}

	if err := handlers.service.SaveAvatar(request.Context(), user.ID, image, mime); err != nil {
		handlers.logger.Error("save avatar failed", "error", err, "customer_id", user.ID)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось сохранить фото",
		})
		return
	}
	handlers.writeCurrentUser(response, request)
}

func (handlers authHandlers) deleteAvatar(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.sessionUser(response, request, "Не удалось удалить фото")
	if !ok {
		return
	}
	if err := handlers.service.DeleteAvatar(request.Context(), user.ID); err != nil {
		handlers.logger.Error("delete avatar failed", "error", err, "customer_id", user.ID)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось удалить фото",
		})
		return
	}
	handlers.writeCurrentUser(response, request)
}

func (handlers authHandlers) avatar(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.sessionUser(response, request, "Не удалось загрузить фото")
	if !ok {
		return
	}
	image, mime, err := handlers.service.Avatar(request.Context(), user.ID)
	if err != nil {
		handlers.logger.Error("load avatar failed", "error", err, "customer_id", user.ID)
		writeJSON(response, http.StatusInternalServerError, errorResponse{
			Error: "Не удалось загрузить фото",
		})
		return
	}
	if len(image) == 0 {
		writeJSON(response, http.StatusNotFound, errorResponse{Error: "Фото не загружено"})
		return
	}
	// Pictures stored before the signature check landed carry whatever type
	// was claimed at upload, so the bytes decide here too.
	if _, allowed := allowedAvatarMimes[mime]; !allowed {
		mime = detectImageMime(image)
	}
	if _, allowed := allowedAvatarMimes[mime]; !allowed {
		writeJSON(response, http.StatusNotFound, errorResponse{Error: "Фото не загружено"})
		return
	}
	response.Header().Set("Content-Type", mime)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Disposition", "inline")
	// Private: the picture is served from the session, never from a shared
	// cache. The URL carries the upload timestamp, so the browser may keep
	// its own copy until the customer uploads a new one.
	response.Header().Set("Cache-Control", "private, max-age=86400")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(image)
}

// sessionUser resolves the signed-in customer, writing the appropriate
// error response and reporting false when there is none.
func (handlers authHandlers) sessionUser(
	response http.ResponseWriter,
	request *http.Request,
	failureMessage string,
) (*auth.User, bool) {
	cookie, err := request.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return nil, false
	}
	user, err := handlers.service.UserByToken(request.Context(), cookie.Value)
	if err != nil {
		handlers.logger.Error("session lookup failed", "error", err)
		writeJSON(response, http.StatusInternalServerError, errorResponse{Error: failureMessage})
		return nil, false
	}
	if user == nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return nil, false
	}
	return user, true
}

// writeCurrentUser re-reads and returns the profile so the page can refresh
// itself from a single response after a change.
func (handlers authHandlers) writeCurrentUser(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return
	}
	user, err := handlers.service.UserByToken(request.Context(), cookie.Value)
	if err != nil || user == nil {
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}
