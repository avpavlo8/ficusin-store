package order

import (
	"context"
	"errors"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
)

func (worker *ShippingWorker) refreshManualTracking(ctx context.Context) {
	carrier, ok := worker.cdek.(interface {
		FindOrderByTrack(context.Context, string) (integration.Shipment, error)
	})
	if !ok {
		return
	}
	rows, err := worker.pool.Query(ctx, `
		SELECT id, order_number, customer_id, cdek_track_number, status
		FROM orders WHERE delivery_method='cdek' AND cdek_create_state='manual'
			AND cdek_track_number<>'' AND status NOT IN ('cancelled','completed')
			AND (cdek_synced_at IS NULL OR cdek_synced_at<=NOW()-INTERVAL '30 seconds')
		ORDER BY cdek_synced_at ASC NULLS FIRST, id LIMIT 50`)
	if err != nil {
		worker.logger.Error("find manual CDEK tracks failed", "error", err)
		return
	}
	type trackedOrder struct {
		id                    int64
		customerID            *int64
		number, track, status string
	}
	orders := []trackedOrder{}
	for rows.Next() {
		var item trackedOrder
		if err = rows.Scan(&item.id, &item.number, &item.customerID, &item.track, &item.status); err != nil {
			rows.Close()
			worker.logger.Error("scan manual CDEK track failed", "error", err)
			return
		}
		orders = append(orders, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		worker.logger.Error("read manual CDEK tracks failed", "error", err)
		return
	}
	for _, item := range orders {
		shipment, lookupErr := carrier.FindOrderByTrack(ctx, item.track)
		if lookupErr == nil && shipment.TrackNumber != item.track {
			lookupErr = errors.New("CDEK returned a different tracking number")
		}
		if lookupErr != nil {
			message := "Не удалось обновить статус по трек-номеру СДЭК. Проверим снова."
			if errors.Is(lookupErr, integration.ErrCDEKOrderNotFound) {
				message = "СДЭК не нашёл отправление по этому трек-номеру. Проверьте номер и договор СДЭК."
			}
			if _, err = worker.pool.Exec(ctx, `UPDATE orders SET cdek_synced_at=NOW(), cdek_last_error=$3
				WHERE id=$1 AND cdek_create_state='manual' AND cdek_track_number=$2 AND status NOT IN ('cancelled','completed')`, item.id, item.track, message); err != nil {
				worker.logger.Error("store manual tracking error failed", "order", item.number, "error", err)
			}
			continue
		}
		next := cdekStatusToOrder[strings.ToUpper(shipment.Status)]
		changed := next != "" && next != item.status
		// Keep the operator's track and any previously owned UUID. This lookup
		// only proves carrier status, not ownership of a cancellable shipment.
		command, err := worker.storeShipmentStatus(ctx, item.id, 0, changed, `
			UPDATE orders SET cdek_status=$3,cdek_status_reason=$4,
				status=COALESCE(NULLIF($5,''),status),cdek_synced_at=NOW(),cdek_last_error=$7
			WHERE id=$1 AND cdek_create_state='manual' AND cdek_track_number=$2 AND status=$6
				AND status NOT IN ('cancelled','completed')`, item.id, item.track, shipment.Status,
			shipment.StatusReason, next, item.status, boundedCDEKError(shipment.RegistrationError))
		if err != nil {
			worker.logger.Error("store manual tracking status failed", "order", item.number, "error", err)
			continue
		}
		if changed && command.RowsAffected() > 0 && item.customerID != nil && worker.notifier != nil {
			_ = worker.notifier.NotifyOrderStatus(ctx, *item.customerID, item.number, next)
		}
	}
}
