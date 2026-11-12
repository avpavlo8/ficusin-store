// Package staticassets prepares immutable, versioned S3 releases of public assets.
// HTML, manifests and the service worker retain their same-origin shell route.
package staticassets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

type Entry struct {
	Path   string `json:"path"`
	Key    string `json:"key"`
	MIME   string `json:"mime"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Prefix string  `json:"prefix"`
	Files  []Entry `json:"files"`
	Bytes  int64   `json:"bytes"`
}

func Build(root, prefix string) (Manifest, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || strings.Contains(prefix, "..") || strings.ContainsAny(prefix, "?\\#") {
		return Manifest{}, fmt.Errorf("provide a versioned safe S3 prefix")
	}
	result := Manifest{Prefix: prefix, Files: []Entry{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not a release asset: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		typ := mime.TypeByExtension(filepath.Ext(rel))
		if typ == "" {
			typ = "application/octet-stream"
		}
		result.Files = append(result.Files, Entry{Path: rel, Key: prefix + "/" + rel, MIME: typ, Bytes: len(data), SHA256: hex.EncodeToString(hash[:])})
		result.Bytes += int64(len(data))
		return nil
	})
	return result, err
}
func excluded(path string) bool {
	switch path {
	case "sw.js", "index.html", "offline.html", "manifest.webmanifest", "admin.webmanifest", "robots.txt":
		return true
	}
	return strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".webmanifest")
}

// Publish is inert unless apply=true. It rechecks local content and verifies
// every remote object's bytes before returning a successful release manifest.
func Publish(ctx context.Context, store fileassets.Storage, root string, manifest Manifest, apply bool) error {
	if !apply {
		return nil
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("asset manifest is empty")
	}
	if store == nil || !store.Configured() {
		return fmt.Errorf("public S3 storage is not configured")
	}
	for _, entry := range manifest.Files {
		if filepath.IsAbs(entry.Path) || strings.Contains(entry.Path, "..") || entry.Key != manifest.Prefix+"/"+entry.Path {
			return fmt.Errorf("invalid manifest path")
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != entry.SHA256 || len(data) != entry.Bytes {
			return fmt.Errorf("asset changed since manifest creation: %s", entry.Path)
		}
		if err = store.Put(ctx, entry.Key, data, entry.MIME); err != nil {
			return fmt.Errorf("upload %s: %w", entry.Path, err)
		}
		remote, err := store.Get(ctx, entry.Key)
		if err != nil {
			return fmt.Errorf("verify %s: %w", entry.Path, err)
		}
		actual := sha256.Sum256(remote)
		if hex.EncodeToString(actual[:]) != entry.SHA256 || len(remote) != entry.Bytes {
			return fmt.Errorf("remote checksum mismatch: %s", entry.Path)
		}
	}
	return nil
}
