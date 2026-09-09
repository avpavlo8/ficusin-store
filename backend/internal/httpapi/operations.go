package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/operations"
)

type operationsReader interface {
	Snapshot(context.Context) (operations.Snapshot, error)
}

func operationsHealthHandler(logger *slog.Logger, reader operationsReader) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if reader == nil {
			writeJSON(response, http.StatusOK, map[string]any{
				"status": "ok", "version": BuildVersion, "criticalChecks": []string{}, "warningChecks": []string{},
			})
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		snapshot, err := reader.Snapshot(ctx)
		if err != nil {
			logger.Error("public commerce operations check failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable", "version": BuildVersion, "criticalChecks": []string{"probe_failed"}, "warningChecks": []string{},
			})
			return
		}
		critical := make([]string, 0)
		warnings := make([]string, 0)
		for _, check := range snapshot.Checks {
			if check.Severity == "critical" {
				critical = append(critical, check.Code)
			} else {
				warnings = append(warnings, check.Code)
			}
		}
		status := http.StatusOK
		if snapshot.Status == "unavailable" {
			status = http.StatusServiceUnavailable
		}
		// Counts and ages are deliberately absent: this endpoint is public and
		// used by the external monitor. Operators get details below.
		writeJSON(response, status, map[string]any{
			"status": snapshot.Status, "version": BuildVersion,
			"criticalChecks": critical, "warningChecks": warnings,
		})
	}
}

func adminOperationsHandler(handlers adminHandlers, reader operationsReader) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := handlers.authorize(response, request, admin.PermissionDashboard)
		if !ok {
			return
		}
		if reader == nil {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Операционная диагностика недоступна"})
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		snapshot, err := reader.Snapshot(ctx)
		if err != nil {
			handlers.failed(response, "admin commerce operations", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"operations": snapshot})
	}
}
