package saby

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StagingStockQuery keeps the copied stock of the isolated staging database
// "live". On production СБИС pushes stock and the storefront hides rows not
// refreshed for two hours; staging receives no pushes, so without this every
// plant turned into a preorder two hours after the copy and the cart and
// storefront checks lost their in-stock product. Rows deliberately voided
// (synced_at at the Unix epoch after unlinking a СБИС card) stay void.
const StagingStockQuery = `UPDATE inventory SET synced_at=CURRENT_TIMESTAMP
	WHERE synced_at>TIMESTAMPTZ '1970-01-02 00:00:00+00'
	  AND synced_at<CURRENT_TIMESTAMP-INTERVAL '20 minutes'`

// StagingStockRefresher is isolated from real production stock sync.
type StagingStockRefresher struct{ pool *pgxpool.Pool }

func NewStagingStockRefresher(ctx context.Context, pool *pgxpool.Pool) (*StagingStockRefresher, error) {
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return nil, err
	}
	if database != "ficusin_staging" && !strings.HasPrefix(database, "ficusin_staging_") {
		return nil, errors.New("stock refresh requires an isolated staging database")
	}
	return &StagingStockRefresher{pool: pool}, nil
}

// Run refreshes copied stock every 15 minutes; deliberately voided rows stay void.
func (p *StagingStockRefresher) Run(ctx context.Context, logger *slog.Logger) {
	refresh := func() {
		tag, err := p.pool.Exec(ctx, StagingStockQuery)
		if err != nil {
			if ctx.Err() == nil {
				logger.Warn("staging stock refresh failed", "error", err)
			}
			return
		}
		if tag.RowsAffected() > 0 {
			logger.Info("staging stock refreshed", "rows", tag.RowsAffected())
		}
	}
	refresh()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
