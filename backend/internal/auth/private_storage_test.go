package auth

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"testing"
)

func TestAvatarUploadRequiresObjectStorage(t *testing.T) {
	if err := NewService(nil, 30, nil).SaveAvatar(context.Background(), 1, []byte("photo"), "image/jpeg"); !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("avatar bypassed object storage", err)
	}
}
