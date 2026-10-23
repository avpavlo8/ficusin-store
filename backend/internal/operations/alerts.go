package operations

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
)

type AlertSender interface {
	SendOperations(context.Context, string) error
}

// AlertDelivery persists successful state transitions across restarts. Stable
// check codes, not ages/counts, prevent repeated notifications every minute.
func (probe *Probe) AlertDelivery(ctx context.Context, sender AlertSender) error {
	return probe.deliverAlert(ctx, sender, probe.Snapshot)
}
func (probe *Probe) deliverAlert(ctx context.Context, sender AlertSender, read func(context.Context) (Snapshot, error)) error {
	if sender == nil {
		return nil
	}
	conn, err := probe.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(704623107)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(704623107)`)
	}()
	snapshot, err := read(ctx)
	if err != nil {
		return err
	}
	fingerprint, message := alertMessage(snapshot)
	var previous string
	err = conn.QueryRow(ctx, `SELECT fingerprint FROM operations_alert_state WHERE id=true`).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if previous == fingerprint {
		return nil
	}
	// First healthy startup is silent; recovery after a problem is actionable.
	if !(previous == "" && snapshot.Status == "ok") {
		if err = sender.SendOperations(ctx, message); err != nil {
			return err
		}
	}
	_, err = conn.Exec(ctx, `INSERT INTO operations_alert_state(id,fingerprint,notified_at) VALUES(true,$1,CURRENT_TIMESTAMP) ON CONFLICT(id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,notified_at=EXCLUDED.notified_at`, fingerprint)
	return err
}
func alertMessage(snapshot Snapshot) (string, string) {
	codes := []string{}
	for _, check := range snapshot.Checks {
		codes = append(codes, check.Severity+":"+check.Code)
	}
	sort.Strings(codes)
	fingerprint := snapshot.Status + "|" + strings.Join(codes, ",")
	if snapshot.Status == "ok" {
		return fingerprint, "Фикусин: операционные проверки восстановились. Подробности — в защищённой панели управления."
	}
	// Only controlled invariant codes leave the server. No order IDs, names,
	// provider payloads, logs or customer data are included.
	return fingerprint, fmt.Sprintf("Фикусин: операционные проверки — %s. Требуется проверка: %s. Подробности — в защищённой панели управления.", snapshot.Status, strings.Join(codes, ", "))
}
