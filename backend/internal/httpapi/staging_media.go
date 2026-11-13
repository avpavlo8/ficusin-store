package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"net/http"
	"time"
)

type stagingPublicMedia interface {
	ReadPublicObject(context.Context, string) (string, []byte, error)
}

func stagingMediaHandler(environment string, storage stagingPublicMedia) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if environment != "staging" || storage == nil {
			http.NotFound(w, r)
			return
		}
		token := r.PathValue("key")
		if len(token) > 2048 {
			http.NotFound(w, r)
			return
		}
		key, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || !fileassets.StagingPublicObjectKey(string(key)) {
			http.NotFound(w, r)
			return
		}
		mime, data, err := storage.ReadPublicObject(r.Context(), string(key))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Ficusin-File-Storage", "staging-mock")
		http.ServeContent(w, r, "staging-image", time.Time{}, bytes.NewReader(data))
	}
}
