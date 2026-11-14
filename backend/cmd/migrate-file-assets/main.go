// migrate-file-assets plans legacy bytea copies by default. --apply is explicit;
// objects are read back and checksummed before committing references. Bytea is
// intentionally retained. No daemon ever runs this command automatically.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/avpavlo8/ficusin-store/backend/internal/fileassets"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"time"
)

func main() {
	apply := flag.Bool("apply", false, "copy verified objects and commit references (default dry-run)")
	publicReviews := flag.Bool("public-reviews", false, "copy legacy-public review objects instead of bytea (source GETs in dry-run)")
	verify := flag.Bool("verify", false, "verify current private references using GET only; exit nonzero if incomplete or blocked")
	limit := flag.Int("limit", 100, "maximum rows per file kind")
	kinds := flag.String("kinds", "", "comma-separated avatars,returns,procurement,reviews,chat (default all)")
	flag.Parse()
	if *verify && (*apply || *publicReviews) {
		fail(fmt.Errorf("--verify cannot be combined with --apply or --public-reviews"))
	}
	if *publicReviews && *kinds != "" && *kinds != "reviews" {
		fail(fmt.Errorf("--public-reviews supports reviews only"))
	}
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	storage := photos.NewPrivateStorage(cfg.PrivatePhotos.Endpoint, cfg.PrivatePhotos.Region, cfg.PrivatePhotos.Bucket, cfg.PrivatePhotos.AccessKey, cfg.PrivatePhotos.SecretKey)
	options := fileassets.MigrationOptions{Apply: *apply, Limit: *limit, Kinds: fileassets.ParseMigrationKinds(*kinds)}
	if *verify {
		report, err := fileassets.VerifyPrivateReferences(ctx, pool, storage, options)
		if err != nil {
			fail(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(report)
		if !report.Ready {
			os.Exit(1)
		}
		return
	}
	var records []fileassets.MigrationRecord
	if *publicReviews {
		if cfg.PrivatePhotos.Bucket == cfg.Photos.Bucket && cfg.PrivatePhotos.Endpoint == cfg.Photos.Endpoint {
			fail(fmt.Errorf("source and private destination must be distinct buckets"))
		}
		source := photos.NewStorage(cfg.Photos.Endpoint, cfg.Photos.Region, cfg.Photos.Bucket, cfg.Photos.AccessKey, cfg.Photos.SecretKey)
		records, err = fileassets.MigrateLegacyPublicReviews(ctx, pool, source, storage, options)
	} else {
		records, err = fileassets.MigrateLegacy(ctx, pool, storage, options)
	}
	result := map[string]any{"dryRun": !*apply, "sourceBytesDeleted": false, "files": records}
	if err != nil {
		result["error"] = err.Error()
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
