package procurement

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"github.com/avpavlo8/ficusin-store/backend/internal/testsupport"
	"testing"
)

func newPrivateTestFileStorage() fileassets.Storage { return testsupport.NewFileStorage() }
func TestImportNeverFallsBackToDatabaseBytes(t *testing.T) {
	_, err := NewPostgresStore(nil).ImportDocument(context.Background(), Actor{}, DocumentUpload{}, ParsedDocument{})
	if !errors.Is(err, fileassets.ErrUnavailable) {
		t.Fatal("new document bypassed object storage", err)
	}
}
