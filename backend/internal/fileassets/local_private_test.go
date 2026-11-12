package fileassets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalPrivateFilesPersistWithoutPublicURL(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private-files")
	first, err := NewLocalPrivateStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, public := any(first).(interface{ PublicURL(string) string }); public {
		t.Fatal("private emulator exposes public file URLs")
	}
	ctx := context.Background()
	key, err := Save(ctx, first, "chat/1", "application/pdf", []byte("private-file"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		path string
		want os.FileMode
	}{{directory, 0700}, {filepath.Join(directory, "chat"), 0700}, {filepath.Join(directory, "chat", "1"), 0700}, {filepath.Join(directory, filepath.FromSlash(key)), 0600}} {
		info, err := os.Stat(entry.path)
		if err != nil || info.Mode().Perm() != entry.want {
			t.Fatal(entry.path, info, err)
		}
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if first.Configured() {
		t.Fatal("closed emulator still configured")
	}
	second, err := NewLocalPrivateStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := Read(ctx, second, key, nil)
	if err != nil || string(got) != "private-file" {
		t.Fatal("restart lost private bytes", string(got), err)
	}
	if err = second.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err = second.Get(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delete did not remove private object", err)
	}
	if err = second.Delete(ctx, key); err != nil {
		t.Fatal("idempotent delete", err)
	}
}
func TestLocalPrivateFilesRejectTraversalAndSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "private")
	outside := filepath.Join(base, "outside")
	os.Mkdir(outside, 0700)
	os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600)
	storage, err := NewLocalPrivateStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	for _, key := range []string{"", "../outside/secret", "/absolute", "a/../../outside/secret", "a//b", "a/./b", `a\b`, "%2e%2e/outside/secret", "a/..", "a\x00b"} {
		if err = storage.Put(ctx, key, []byte("change"), "text/plain"); err == nil {
			t.Errorf("write accepted unsafe key %q", key)
		}
		if _, err = storage.Get(ctx, key); err == nil {
			t.Errorf("read accepted unsafe key %q", key)
		}
		if err = storage.Delete(ctx, key); err == nil {
			t.Errorf("delete accepted unsafe key %q", key)
		}
	}
	os.Symlink(outside, filepath.Join(directory, "escape"))
	if err = storage.Put(ctx, "escape/new", []byte("change"), "text/plain"); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(directory, "secret"))
	if _, err = storage.Get(ctx, "secret"); err == nil {
		t.Fatal("symlink read escaped root")
	}
	if err = storage.Delete(ctx, "secret"); err == nil {
		t.Fatal("symlink delete accepted")
	}
	data, _ := os.ReadFile(filepath.Join(outside, "secret"))
	if string(data) != "outside" {
		t.Fatal("external file changed")
	}
	if _, err = NewLocalPrivateStorage("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err = NewLocalPrivateStorage(string(filepath.Separator)); err == nil {
		t.Fatal("filesystem root accepted")
	}
}
