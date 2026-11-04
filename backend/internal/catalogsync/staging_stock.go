package catalogsync

import (
	"context"
	"log/slog"
	"time"
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

// RunStagingStock refreshes staging stock every 15 minutes until ctx ends.
// It requires the StagingProvider, which has already verified that the
// database is the isolated staging one.
func (p *StagingProvider) RunStagingStock(ctx context.Context, logger *slog.Logger) {
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
