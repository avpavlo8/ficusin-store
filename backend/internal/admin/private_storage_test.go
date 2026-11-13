package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"github.com/avpavlo8/ficusin-store/backend/internal/testsupport"
	"image"
	"image/png"
	"testing"
)

func newPrivateReturnFileStorage() fileassets.Storage { return testsupport.NewFileStorage() }

func TestReturnPhotoRequiresPrivateStorage(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	_, err := NewPostgresRepository(nil).AddReturnPhoto(context.Background(), Actor{Role: RoleOwner}, 1, "image/png", base64.StdEncoding.EncodeToString(imageBytes.Bytes()))
	if !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("new return photo bypassed object storage", err)
	}
}
