// Package testsupport provides in-memory dependencies for local tests only.
// Application constructors never install these substitutes automatically.
package testsupport

import (
	"context"
	"errors"
	"sync"
)

type FileStorage struct {
	mu    sync.Mutex
	files map[string][]byte
	puts  int
}

func NewFileStorage() *FileStorage      { return &FileStorage{files: map[string][]byte{}} }
func (s *FileStorage) Configured() bool { return true }
func (s *FileStorage) Put(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = append([]byte{}, data...)
	s.puts++
	return nil
}
func (s *FileStorage) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[key]
	if !ok {
		return nil, errors.New("fixture object missing")
	}
	return append([]byte{}, data...), nil
}
func (s *FileStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, key)
	return nil
}
func (s *FileStorage) Puts() int { s.mu.Lock(); defer s.mu.Unlock(); return s.puts }
