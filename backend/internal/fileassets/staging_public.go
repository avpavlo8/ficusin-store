package fileassets

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StagingPublicStorage is a separate public-image test namespace. It cannot
// read private_file_mock_objects or accept private attachment keys.
type StagingPublicStorage struct {
	pool *pgxpool.Pool
	site string
}

func NewStagingPublicStorage(ctx context.Context, pool *pgxpool.Pool, site string) (*StagingPublicStorage, error) {
	if _, err := NewStagingPrivateStorage(ctx, pool); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(site)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "ficusin.ru" || parsed.Hostname() == "www.ficusin.ru" {
		return nil, errors.New("staging public images require a separate staging HTTPS SITE_URL")
	}
	return &StagingPublicStorage{pool: pool, site: strings.TrimRight(site, "/")}, nil
}
func StagingPublicObjectKey(key string) bool {
	return validLocalObjectKey(key) && (strings.HasPrefix(key, "products/") || strings.HasPrefix(key, "collections/"))
}
func (s *StagingPublicStorage) Configured() bool { return s != nil && s.pool != nil && s.site != "" }
func (s *StagingPublicStorage) PublicURL(key string) string {
	if !s.Configured() || !StagingPublicObjectKey(key) {
		return ""
	}
	return s.site + "/api/v1/staging/media/" + base64.RawURLEncoding.EncodeToString([]byte(key))
}
func (s *StagingPublicStorage) Put(ctx context.Context, key string, data []byte, mime string) error {
	if !s.Configured() {
		return ErrUnavailable
	}
	if !StagingPublicObjectKey(key) || len(data) == 0 || len(data) > 64<<20 {
		return errors.New("invalid staging public image")
	}
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return errors.New("staging public storage accepts product images only")
	}
	sum := sha256.Sum256(data)
	_, err := s.pool.Exec(ctx, `INSERT INTO public_file_mock_objects(object_key,content_type,body,sha256)VALUES($1,$2,$3,$4) ON CONFLICT(object_key) DO UPDATE SET content_type=EXCLUDED.content_type,body=EXCLUDED.body,sha256=EXCLUDED.sha256,updated_at=NOW()`, key, mime, data, hex.EncodeToString(sum[:]))
	return err
}
func (s *StagingPublicStorage) ReadPublicObject(ctx context.Context, key string) (string, []byte, error) {
	if !s.Configured() {
		return "", nil, ErrUnavailable
	}
	if !StagingPublicObjectKey(key) {
		return "", nil, errors.New("not a public staging image key")
	}
	var data []byte
	var mime, checksum string
	if err := s.pool.QueryRow(ctx, `SELECT content_type,body,sha256 FROM public_file_mock_objects WHERE object_key=$1`, key).Scan(&mime, &data, &checksum); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != checksum {
		return "", nil, errors.New("staging public image checksum mismatch")
	}
	return mime, data, nil
}
func (s *StagingPublicStorage) Get(ctx context.Context, key string) ([]byte, error) {
	_, data, err := s.ReadPublicObject(ctx, key)
	return data, err
}
func (s *StagingPublicStorage) Delete(ctx context.Context, key string) error {
	if !s.Configured() {
		return ErrUnavailable
	}
	if !StagingPublicObjectKey(key) {
		return errors.New("not a public staging image key")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM public_file_mock_objects WHERE object_key=$1`, key)
	return err
}
