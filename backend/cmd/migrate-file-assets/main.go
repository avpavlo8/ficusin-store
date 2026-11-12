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
	limit := flag.Int("limit", 100, "maximum rows per file kind")
	kinds := flag.String("kinds", "", "comma-separated avatars,returns,procurement,finance,reviews (default all)")
	flag.Parse()
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
	records, err := fileassets.MigrateLegacy(ctx, pool, storage, fileassets.MigrationOptions{Apply: *apply, Limit: *limit, Kinds: fileassets.ParseMigrationKinds(*kinds)})
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
