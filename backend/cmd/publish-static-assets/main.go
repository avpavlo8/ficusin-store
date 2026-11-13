// publish-static-assets emits a manifest without uploading unless --apply is set.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/avpavlo8/ficusin-store/backend/internal/staticassets"
	"os"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "frontend/dist", "built frontend directory")
	prefix := flag.String("prefix", "site/releases/preview", "unique immutable release prefix")
	apply := flag.Bool("apply", false, "upload and verify files (default dry-run)")
	out := flag.String("manifest", "", "optional JSON manifest file; default stdout")
	flag.Parse()
	manifest, err := staticassets.Build(*dir, *prefix)
	if err != nil {
		fail(err)
	}
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	if endpoint == "" {
		endpoint = "https://s3.twcstorage.ru"
	}
	region := strings.TrimSpace(os.Getenv("S3_REGION"))
	if region == "" {
		region = "ru-1"
	}
	storage := photos.NewStorage(endpoint, region, os.Getenv("S3_BUCKET"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if *apply && *prefix == "site/releases/preview" {
		fail(fmt.Errorf("--apply requires an explicit unique --prefix"))
	}
	if err = staticassets.Publish(ctx, storage, *dir, manifest, *apply); err != nil {
		fail(err)
	}
	result := map[string]any{"dryRun": !*apply, "verified": *apply, "manifest": manifest}
	writer := os.Stdout
	if *out != "" {
		writer, err = os.Create(*out)
		if err != nil {
			fail(err)
		}
		defer writer.Close()
	}
	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	if err = enc.Encode(result); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
