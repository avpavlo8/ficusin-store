// Package operations verifies the commerce invariants that must remain true
// even when a provider, worker, or process fails halfway through its work.
package operations

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const snapshotSQL = `
WITH
inventory_totals AS (
  SELECT variant_id, SUM(reserved_qty)::bigint AS quantity
  FROM inventory
  GROUP BY variant_id
),
order_totals AS (
  SELECT item.variant_id, SUM(item.reserved_qty)::bigint AS quantity
  FROM order_items item
  JOIN orders purchase ON purchase.id = item.order_id
  WHERE item.variant_id IS NOT NULL
    AND purchase.stock_released_at IS NULL
  GROUP BY item.variant_id
),
reservation_mismatches AS (
  SELECT COALESCE(inventory.variant_id, orders.variant_id) AS variant_id
  FROM inventory_totals inventory
  FULL OUTER JOIN order_totals orders USING (variant_id)
  WHERE COALESCE(inventory.quantity, 0) <> COALESCE(orders.quantity, 0)
)
SELECT code, severity, affected, oldest_seconds
FROM (
  SELECT 'reservation_ledger_mismatch'::text AS code, 'critical'::text AS severity,
         COUNT(*)::bigint AS affected, NULL::bigint AS oldest_seconds
  FROM reservation_mismatches
  UNION ALL
  SELECT 'negative_inventory_reservation', 'critical', COUNT(*)::bigint, NULL::bigint
  FROM inventory WHERE reserved_qty < 0
  UNION ALL
  SELECT 'duplicate_pending_payments', 'critical', COUNT(*)::bigint, NULL::bigint
  FROM (
    SELECT order_id FROM payments WHERE status = 'pending'
    GROUP BY order_id HAVING COUNT(*) > 1
  ) duplicates
  UNION ALL
  SELECT 'provider_paid_order_unsettled', 'critical', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(payment.updated_at)))::bigint
  FROM payments payment
  JOIN orders purchase ON purchase.id = payment.order_id
  WHERE payment.status = 'paid'
    AND purchase.payment_method = 'online'
    AND purchase.status <> 'cancelled'
    AND purchase.payment_status NOT IN ('paid', 'partially_paid', 'refunded')
  UNION ALL
  SELECT 'stock_reservation_journal_mismatch', 'warning', COUNT(*)::bigint, NULL::bigint
  FROM (
    SELECT item.order_id, item.variant_id
    FROM order_items item
    JOIN orders purchase ON purchase.id = item.order_id
    LEFT JOIN stock_movements movement
      ON movement.order_id = item.order_id
     AND movement.variant_id = item.variant_id
     AND movement.kind IN ('reserve', 'release')
    WHERE purchase.created_at >= CURRENT_TIMESTAMP - INTERVAL '30 days'
    GROUP BY item.order_id, item.variant_id
    HAVING MAX(CASE WHEN purchase.stock_released_at IS NULL THEN item.reserved_qty ELSE 0 END)
      <> COALESCE(SUM(CASE WHEN movement.kind = 'reserve' THEN movement.quantity ELSE -movement.quantity END), 0)
  ) journal_mismatches
  UNION ALL
  SELECT 'inventory_overreserved', 'warning', COUNT(*)::bigint, NULL::bigint
  FROM inventory WHERE reserved_qty > GREATEST(available_qty, 0)
  UNION ALL
  SELECT 'stale_pending_payment', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(payment.created_at)))::bigint
  FROM payments payment
  JOIN orders purchase ON purchase.id = payment.order_id
  WHERE payment.status = 'pending' AND purchase.status <> 'cancelled'
    AND payment.created_at < CURRENT_TIMESTAMP - INTERVAL '30 minutes'
  UNION ALL
  SELECT 'stale_outbox', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM outbox
  WHERE sent_at IS NULL AND cancelled_at IS NULL AND attempts < 5
    AND created_at < CURRENT_TIMESTAMP - INTERVAL '15 minutes'
  UNION ALL
  SELECT 'exhausted_outbox', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM outbox
  WHERE sent_at IS NULL AND cancelled_at IS NULL AND attempts >= 5
  UNION ALL
  SELECT 'cdek_manual_review', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at)))::bigint
  FROM orders WHERE cdek_create_state = 'manual_review' OR cdek_cancel_state = 'manual_review'
  UNION ALL
  SELECT 'cdek_retry_overdue', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(cdek_next_attempt_at)))::bigint
  FROM orders
  WHERE cdek_create_state = 'retry'
    AND cdek_next_attempt_at < CURRENT_TIMESTAMP - INTERVAL '5 minutes'
  UNION ALL
  SELECT CASE failed.channel
           WHEN 'wb' THEN 'failed_procurement_action_wb'
           WHEN 'ozon' THEN 'failed_procurement_action_ozon'
           WHEN 'saby_receipt' THEN
             CASE
               WHEN NULLIF(BTRIM(COALESCE(failed.external_operation_id, '')), '') IS NULL
                 THEN 'failed_procurement_action_saby_receipt_not_started'
               WHEN BTRIM(failed.external_operation_id) ~ '^[0-9]+$'
                 THEN 'failed_procurement_action_saby_receipt_retail_started'
               ELSE 'failed_procurement_action_saby_receipt_legacy_id'
             END
           WHEN 'saby_price' THEN 'failed_procurement_action_saby_price'
           ELSE 'failed_procurement_action_other'
         END,
         'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(failed.created_at)))::bigint
  FROM procurement_action_items failed
  JOIN procurement_action_batches failed_batch ON failed_batch.id = failed.batch_id
  WHERE failed.status = 'failed'
    AND failed_batch.status <> 'cancelled'
    AND NOT EXISTS (
      SELECT 1
      FROM procurement_action_items resolved
      WHERE resolved.procurement_order_line_id = failed.procurement_order_line_id
        AND resolved.channel = failed.channel
        AND resolved.id > failed.id
        AND resolved.status = 'completed'
    )
  GROUP BY 1
  UNION ALL
  SELECT 'expired_procurement_lock', 'warning', COUNT(*)::bigint,
         EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(locked_until)))::bigint
  FROM procurement_action_items
  WHERE status = 'processing' AND locked_until < CURRENT_TIMESTAMP
) checks
WHERE affected > 0
ORDER BY severity, code`

type Check struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	Affected      int64  `json:"affected"`
	OldestSeconds *int64 `json:"oldestSeconds,omitempty"`
}

type Snapshot struct {
	Status      string    `json:"status"`
	GeneratedAt time.Time `json:"generatedAt"`
	Checks      []Check   `json:"checks"`
}

type Probe struct {
	pool *pgxpool.Pool
}

func NewProbe(pool *pgxpool.Pool) *Probe { return &Probe{pool: pool} }

func (probe *Probe) Snapshot(ctx context.Context) (Snapshot, error) {
	rows, err := probe.pool.Query(ctx, snapshotSQL)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()

	checks := make([]Check, 0)
	for rows.Next() {
		var check Check
		if err := rows.Scan(&check.Code, &check.Severity, &check.Affected, &check.OldestSeconds); err != nil {
			return Snapshot{}, err
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Status: statusFor(checks), GeneratedAt: time.Now().UTC(), Checks: checks}, nil
}

func statusFor(checks []Check) string {
	status := "ok"
	for _, check := range checks {
		if check.Severity == "critical" {
			return "unavailable"
		}
		status = "degraded"
	}
	return status
}

// Run emits one structured snapshot immediately and then every minute. The
// external monitor is the pager; these records preserve the diagnosis even if
// the condition clears before an engineer opens the incident.
func (probe *Probe) Run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		probe.log(ctx, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (probe *Probe) log(ctx context.Context, logger *slog.Logger) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snapshot, err := probe.Snapshot(checkCtx)
	if err != nil {
		logger.Error("commerce operations probe failed", "error", err)
		return
	}
	attributes := []any{"status", snapshot.Status, "check_count", len(snapshot.Checks)}
	for _, check := range snapshot.Checks {
		attributes = append(attributes, "check."+check.Code, check.Affected)
	}
	logger.Info("commerce operations snapshot", attributes...)
}
