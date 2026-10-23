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
				"status": "ok", "version": BuildVersion,
			})
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		snapshot, err := reader.Snapshot(ctx)
		if err != nil {
			logger.Error("public commerce operations check failed", "error", err)
			writeJSON(response, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable", "version": BuildVersion,
			})
			return
		}
		status := http.StatusOK
		if snapshot.Status == "unavailable" {
			status = http.StatusServiceUnavailable
		}
		// Public monitors receive status only; internal check codes, counts and
		// ages remain behind the authenticated admin endpoint.
		writeJSON(response, status, map[string]any{
			"status": snapshot.Status, "version": BuildVersion,
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
		result := map[string]any{"operations": snapshot}
		if diagnosticReader, ok := reader.(interface {
			Diagnose(context.Context) (operations.Diagnostics, error)
		}); ok {
			diagnostics, err := diagnosticReader.Diagnose(ctx)
			if err != nil {
				handlers.failed(response, "admin commerce diagnostics", err)
				return
			}
			result["diagnostics"] = diagnostics
		}
		writeJSON(response, http.StatusOK, result)
	}
}
