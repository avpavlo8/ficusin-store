package fileassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

var stagingFileDatabase = regexp.MustCompile(`^ficusin_staging(?:_[a-z0-9_]+)?$`)

// StagingPrivateStorage is an explicitly selected test provider. It persists
// test objects in the isolated staging database so deploys cannot lose uploads.
// No object URL or network route exists; normal API access checks still apply.
// Production never chooses this provider or silently falls back to DB bytes.
type StagingPrivateStorage struct{ pool *pgxpool.Pool }

func NewStagingPrivateStorage(ctx context.Context, pool *pgxpool.Pool) (*StagingPrivateStorage, error) {
	if pool == nil {
		return nil, ErrUnavailable
	}
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return nil, err
	}
	if !stagingFileDatabase.MatchString(database) {
		return nil, errors.New("staging file mock requires an isolated ficusin_staging database")
	}
	return &StagingPrivateStorage{pool: pool}, nil
}
func (s *StagingPrivateStorage) Configured() bool { return s != nil && s.pool != nil }
func (s *StagingPrivateStorage) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if !s.Configured() {
		return ErrUnavailable
	}
	if !validLocalObjectKey(key) || len(data) == 0 || len(data) > 64<<20 {
		return errors.New("invalid staging object")
	}
	sum := sha256.Sum256(data)
	_, err := s.pool.Exec(ctx, `INSERT INTO private_file_mock_objects(object_key,content_type,body,sha256)VALUES($1,$2,$3,$4) ON CONFLICT(object_key) DO UPDATE SET content_type=EXCLUDED.content_type,body=EXCLUDED.body,sha256=EXCLUDED.sha256,updated_at=NOW()`, key, contentType, data, hex.EncodeToString(sum[:]))
	return err
}
func (s *StagingPrivateStorage) Get(ctx context.Context, key string) ([]byte, error) {
	if !s.Configured() {
		return nil, ErrUnavailable
	}
	if !validLocalObjectKey(key) {
		return nil, errors.New("invalid staging object key")
	}
	var body []byte
	var expected string
	if err := s.pool.QueryRow(ctx, `SELECT body,sha256 FROM private_file_mock_objects WHERE object_key=$1`, key).Scan(&body, &expected); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("staging object checksum mismatch")
	}
	return body, nil
}
func (s *StagingPrivateStorage) Delete(ctx context.Context, key string) error {
	if !s.Configured() {
		return ErrUnavailable
	}
	if !validLocalObjectKey(key) {
		return errors.New("invalid staging object key")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM private_file_mock_objects WHERE object_key=$1`, key)
	return err
}
