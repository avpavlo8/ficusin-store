package fileassets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// LocalPrivateStorage is an explicit development-only object-store emulator.
// It exposes no URL and is never registered with a public FileServer. Callers
// retain their normal authenticated API checks before Get.
type LocalPrivateStorage struct {
	root   *os.Root
	closed atomic.Bool
}

func NewLocalPrivateStorage(directory string) (*LocalPrivateStorage, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) == string(filepath.Separator) {
		return nil, errors.New("local private storage requires a dedicated absolute directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("private storage root must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	err = dir.Chmod(0700)
	dir.Close()
	if err != nil {
		root.Close()
		return nil, err
	}
	return &LocalPrivateStorage{root: root}, nil
}
func (s *LocalPrivateStorage) Configured() bool { return s != nil && s.root != nil && !s.closed.Load() }
func (s *LocalPrivateStorage) Close() error {
	if s == nil || s.root == nil || s.closed.Swap(true) {
		return nil
	}
	return s.root.Close()
}

func validLocalObjectKey(key string) bool {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || path.Clean(key) != key {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return false
			}
		}
	}
	return true
}
func (s *LocalPrivateStorage) checkKey(key string, create bool) error {
	if !s.Configured() {
		return ErrUnavailable
	}
	if !validLocalObjectKey(key) {
		return errors.New("invalid private object key")
	}
	segments := strings.Split(key, "/")
	for i := 1; i < len(segments); i++ {
		parent := strings.Join(segments[:i], "/")
		if create {
			if err := s.root.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
		info, err := s.root.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("private storage symlink or non-directory ancestor")
		}
		if create {
			dir, err := s.root.Open(parent)
			if err != nil {
				return err
			}
			err = dir.Chmod(0700)
			dir.Close()
			if err != nil {
				return err
			}
		}
	}
	if info, err := s.root.Lstat(key); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("private object must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (s *LocalPrivateStorage) Put(ctx context.Context, key string, data []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > 64<<20 {
		return errors.New("private object exceeds 64 MiB")
	}
	if err := s.checkKey(key, true); err != nil {
		return err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	temporary := path.Join(path.Dir(key), ".upload-"+hex.EncodeToString(token))
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		file.Close()
		if !keep {
			_ = s.root.Remove(temporary)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.root.Rename(temporary, key); err != nil {
		return err
	}
	keep = true
	return nil
}
func (s *LocalPrivateStorage) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkKey(key, false); err != nil {
		return nil, err
	}
	file, err := s.root.Open(key)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("private object is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("private object exceeds 64 MiB")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
func (s *LocalPrivateStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkKey(key, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	err := s.root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
