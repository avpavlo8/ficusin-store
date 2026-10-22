package httpapi

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/chat"
	"github.com/jackc/pgx/v5"
)

const chatCookieName = "ficusin_chat_session"
const chatConsentVersion = "2026-10-22-chat"

type chatHandlers struct {
	store   *chat.Store
	admin   adminHandlers
	uploads *rateLimiter
	secure  bool
}

func (h chatHandlers) identity(w http.ResponseWriter, r *http.Request, create bool) (chat.Identity, bool) {
	w.Header().Set("Cache-Control", "no-store")
	who := chat.Identity{}
	if cookie, err := r.Cookie(auth.CookieName); err == nil && h.admin.auth != nil {
		user, err := h.admin.auth.UserByToken(r.Context(), cookie.Value)
		if err != nil {
			h.fail(w, err)
			return who, false
		}
		if user != nil {
			who.CustomerID = user.ID
		}
	}
	token := ""
	if cookie, err := r.Cookie(chatCookieName); err == nil && len(cookie.Value) == 43 {
		token = cookie.Value
	}
	if token == "" && create {
		var err error
		token, err = newCartToken()
		if err != nil {
			h.fail(w, err)
			return who, false
		}
		http.SetCookie(w, &http.Cookie{Name: chatCookieName, Value: token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: 365 * 86400, Expires: time.Now().Add(365 * 24 * time.Hour)})
	}
	if token != "" {
		who.GuestHash = hashCartToken(token)
	}
	return who, true
}
func (h chatHandlers) fail(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := "Не удалось загрузить чат. Попробуйте ещё раз"
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusNotFound
		message = "Переписка не найдена"
	}
	if errors.Is(err, chat.ErrInvalid) {
		status = http.StatusBadRequest
		message = "Проверьте сообщение: до 4000 символов, вложение до 20 МБ"
	}
	if errors.Is(err, chat.ErrConsent) {
		status = http.StatusBadRequest
		message = "Подтвердите согласие на обработку данных для начала переписки"
	}
	if status == http.StatusServiceUnavailable {
		h.admin.logger.Error("chat operation failed", "error", err)
	}
	writeJSON(w, status, errorResponse{Error: message})
}
func (h chatHandlers) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	staff := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	var staffID, id int64
	who := chat.Identity{}
	if staff {
		user, _, ok := h.admin.authorize(w, r, admin.PermissionChat)
		if !ok {
			return
		}
		staffID = user.ID
		if r.PathValue("id") != "" {
			var ok bool
			id, ok = pathID(w, r)
			if !ok {
				return
			}
		}
	} else {
		var ok bool
		who, ok = h.identity(w, r, r.URL.Path == "/api/v1/chat" || (r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages")))
		if !ok {
			return
		}
	}
	if strings.HasSuffix(r.URL.Path, "/unread") {
		if !staff {
			var err error
			id, err = h.store.Resolve(r.Context(), who)
			if err != nil {
				h.fail(w, err)
				return
			}
		}
		count, err := h.store.Unread(r.Context(), id, staff)
		if err != nil {
			h.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"unread": count})
		return
	}
	if staff && r.URL.Path == "/api/v1/admin/chat" {
		status := r.URL.Query().Get("status")
		if status != "" && status != "open" && status != "closed" {
			h.fail(w, chat.ErrInvalid)
			return
		}
		query := r.URL.Query().Get("q")
		if len(query) > 200 {
			h.fail(w, chat.ErrInvalid)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		items, err := h.store.List(r.Context(), status, query, offset)
		if err != nil {
			h.fail(w, err)
			return
		}
		more := len(items) > 50
		if more {
			items = items[:50]
		}
		writeJSON(w, 200, map[string]any{"items": items, "hasMore": more})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost {
		if r.ContentLength > 3*1024*1024 {
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now().Add(90 * time.Second))
			_ = controller.SetWriteDeadline(time.Now().Add(90 * time.Second))
		}
		var input chat.Input
		if decodeJSONWithLimit(r, &input, 29*1024*1024) != nil {
			h.fail(w, chat.ErrInvalid)
			return
		}
		if input.Attachment != nil && !staff && h.uploads != nil && !h.uploads.allow(clientIP(r)) {
			writeJSON(w, 429, errorResponse{Error: "Слишком много вложений. Подождите минуту"})
			return
		}
		input.ConsentVersion = chatConsentVersion
		input.IP = clientIP(r)
		input.Agent = string([]rune(r.UserAgent())[:min(500, len([]rune(r.UserAgent())))])
		id, err := h.store.Send(r.Context(), who, staffID, id, input)
		if err != nil {
			h.fail(w, err)
			return
		}
		thread, err := h.store.Read(r.Context(), id, 0)
		if err != nil {
			h.fail(w, err)
			return
		}
		writeJSON(w, 200, thread)
		return
	}
	if !staff {
		var err error
		id, err = h.store.Resolve(r.Context(), who)
		if err != nil {
			h.fail(w, err)
			return
		}
	}
	if r.PathValue("messageID") != "" {
		messageID, err := strconv.ParseInt(r.PathValue("messageID"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		kind, name, attachmentKind, data, err := h.store.Attachment(r.Context(), id, messageID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/photos/") && attachmentKind != "image" {
			http.NotFound(w, r)
			return
		}
		disposition := "inline"
		if attachmentKind == "file" {
			disposition = "attachment"
			kind = "application/octet-stream"
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(90 * time.Second))
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/read") {
		var body struct {
			Through int64 `json:"through"`
		}
		if decodeJSON(r, &body) != nil || body.Through < 0 {
			h.fail(w, chat.ErrInvalid)
			return
		}
		if err := h.store.MarkRead(r.Context(), id, body.Through, staff); err != nil {
			h.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == http.MethodPatch {
		var body struct {
			Status string `json:"status"`
		}
		if decodeJSON(r, &body) != nil {
			h.fail(w, chat.ErrInvalid)
			return
		}
		if err := h.store.SetStatus(r.Context(), id, body.Status); err != nil {
			h.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if before < 0 {
		before = 0
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	var thread chat.Thread
	var err error
	if after > 0 {
		thread, err = h.store.ReadAfter(r.Context(), id, after)
	} else {
		thread, err = h.store.Read(r.Context(), id, before)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	thread.ConsentRequired = !staff && who.CustomerID == 0 && thread.ID == 0
	writeJSON(w, 200, thread)
}
