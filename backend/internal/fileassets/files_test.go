package fileassets

import (
	"context"
	"errors"
	"testing"
)

type fake struct {
	data    map[string][]byte
	corrupt bool
	deleted int
}

func (f *fake) Configured() bool { return true }
func (f *fake) Put(_ context.Context, k string, b []byte, _ string) error {
	f.data[k] = append([]byte{}, b...)
	return nil
}
func (f *fake) Get(_ context.Context, k string) ([]byte, error) {
	if f.corrupt {
		return []byte("corrupt"), nil
	}
	b, ok := f.data[k]
	if !ok {
		return nil, errors.New("missing")
	}
	return b, nil
}
func (f *fake) Delete(_ context.Context, k string) error { delete(f.data, k); f.deleted++; return nil }
func TestVerifiedSaveAndAuthoritativeObjectRead(t *testing.T) {
	ctx := context.Background()
	s := &fake{data: map[string][]byte{}}
	key, err := Save(ctx, s, "documents/42", "application/pdf", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx, s, key)
	if err != nil || string(got) != "original" {
		t.Fatal(string(got), err)
	}
	if _, err = Read(ctx, nil, key); !errors.Is(err, ErrUnavailable) {
		t.Fatal("S3 outage revived legacy data", err)
	}
	got, err = Read(ctx, nil, "")
	if err != nil || got != nil {
		t.Fatal(err)
	}
	s.corrupt = true
	if _, err = Save(ctx, s, "avatars/1", "image/jpeg", []byte("photo")); err == nil || s.deleted != 1 {
		t.Fatal("corrupt upload was accepted or leaked", err, s.deleted)
	}
	if _, err = Save(ctx, nil, "avatars/1", "image/jpeg", []byte("photo")); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unconfigured upload silently fell back", err)
	}
}
