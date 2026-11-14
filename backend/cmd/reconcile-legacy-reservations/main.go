// reconcile-legacy-reservations defaults to a read-only inventory. Its explicit
// apply pilot accepts only a disposable loopback database, never production.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/integration"
	"github.com/avpavlo8/ficusin-store/backend/internal/reservationtransition"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	apply := flag.Bool("apply", false, "apply one reviewed safe group (disposable loopback DB only)")
	expected := flag.String("expected-database", "", "required exact current_database guard")
	variant := flag.Int64("variant-id", 0, "reviewed reservation group")
	fingerprint := flag.String("expected-fingerprint", "", "exact fingerprint from dry-run")
	limit := flag.Int("limit", 1000, "maximum groups in the read-only report")
	output := flag.String("report", "", "optional private JSON file with group IDs/fingerprints")
	verifyPayments := flag.Bool("verify-payments", false, "explicit read-only GET observations; never authorizes reservation apply")
	flag.Parse()
	if *expected == "" {
		fail("expected_database_required")
	}
	if *apply && *verifyPayments {
		fail("payment_reads_do_not_authorize_apply")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail("database_connection_failed")
	}
	defer pool.Close()
	var db string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db); err != nil {
		fail("database_connection_failed")
	}
	if db != *expected {
		fail("database_guard_mismatch")
	}
	if *apply {
		result, e := reservationtransition.ApplyLocal(ctx, pool, *variant, *fingerprint)
		if e != nil {
			switch {
			case errors.Is(e, reservationtransition.ErrLocalOnly):
				fail("local_disposable_apply_only")
			case errors.Is(e, reservationtransition.ErrChanged):
				fail("snapshot_changed")
			case errors.Is(e, reservationtransition.ErrBlocked):
				fail("blocked_group")
			case errors.Is(e, reservationtransition.ErrOutcomeUnknown):
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"operationKey": result.OperationKey, "outcomeUnknown": true, "providerWrites": false, "physicalStockChanged": false})
				fail("outcome_unknown_inspect_same_operation_key")
			default:
				fail("reconciliation_failed")
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return
	}
	report, err := reservationtransition.Audit(ctx, pool, *limit)
	if err != nil {
		fail("read_only_audit_failed")
	}
	if *verifyPayments {
		reader := integration.NewYooKassaClient(os.Getenv("YOOKASSA_SHOP_ID"), os.Getenv("YOOKASSA_SECRET_KEY"), 0, 1)
		if !reader.Configured() {
			fail("payment_read_keys_missing")
		}
		observed, e := reservationtransition.ReadUnfinishedPayments(ctx, pool, reader, *limit)
		if e != nil {
			fail("payment_read_only_observation_failed")
		}
		report.PaymentReads = &observed
	}
	if *output != "" {
		raw, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			fail("report_encoding_failed")
		}
		f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			fail("report_create_failed")
		}
		_, e = f.Write(raw)
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			fail("report_write_failed")
		}
	}
	// CI output contains aggregate counts only. Provider IDs, customer data,
	// connection strings and individual snapshots are never printed.
	report.Groups = nil
	_ = json.NewEncoder(os.Stdout).Encode(report)
}
func fail(code string) { _, _ = fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", code); os.Exit(1) }
