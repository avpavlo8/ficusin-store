package reviews

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"image"
	"image/png"
	"testing"
)

func TestReviewMediaDoesNotFallBackToDatabase(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_, err := NewStore(nil).Create(context.Background(), 1, "81", Input{Rating: 5, Text: "Достаточно длинный отзыв", Photos: []MediaInput{{ContentType: "image/png", Data: base64.StdEncoding.EncodeToString(b.Bytes())}}})
	if !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("review upload bypassed object storage", err)
	}
}
