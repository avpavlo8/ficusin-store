package staticassets

import (
	"context"
	"github.com/avpavlo8/ficusin-store/backend/internal/testsupport"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestDryRunAndChangedAssetGuard(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "assets"), 0700)
	os.WriteFile(filepath.Join(root, "assets", "main.js"), []byte("version 1"), 0600)
	os.WriteFile(filepath.Join(root, "sw.js"), []byte("worker"), 0600)
	os.WriteFile(filepath.Join(root, "index.html"), []byte("HTML"), 0600)
	manifest, err := Build(root, "site/releases/test")
	if err != nil || len(manifest.Files) != 1 || manifest.Files[0].Key != "site/releases/test/assets/main.js" {
		t.Fatal(manifest, err)
	}
	if err = Publish(context.Background(), nil, root, manifest, false); err != nil {
		t.Fatal("dry-run requires no storage", err)
	}
	if err = Publish(context.Background(), nil, root, manifest, true); err == nil {
		t.Fatal("apply accepted missing storage")
	}
	storage := testsupport.NewFileStorage()
	if err = Publish(context.Background(), storage, root, manifest, true); err != nil || storage.Puts() != 1 {
		t.Fatal("verified publish failed", err)
	}
	if data, err := storage.Get(context.Background(), manifest.Files[0].Key); err != nil || string(data) != "version 1" {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "assets", "main.js"), []byte("version 2"), 0600)
	if err = Publish(context.Background(), storage, root, manifest, true); err == nil || storage.Puts() != 1 {
		t.Fatal("changed file was published under an old manifest", err)
	}
	if _, err = Build(root, "../escape"); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
}
