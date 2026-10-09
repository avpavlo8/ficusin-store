package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

type manualPriceProposalRepository interface {
	ListManualPriceProposals(context.Context, int64) ([]admin.PriceProposal, error)
	ApproveManualPriceProposal(context.Context, admin.Actor, int64) (admin.PriceProposal, error)
	ManualSabyPriceXLSX(context.Context, int64) ([]byte, string, error)
}

func manualSabyPriceXLSXHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok {
			return
		}
		id, ok := pimPathID(response, request, "proposalId")
		if !ok {
			return
		}
		repository, ok := adminAPI.repository.(manualPriceProposalRepository)
		if !ok {
			adminAPI.failed(response, "manual price proposals unavailable", errors.New("manual price proposals unavailable"))
			return
		}
		content, name, err := repository.ManualSabyPriceXLSX(request.Context(), id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(response, http.StatusNotFound, errorResponse{Error: "Подтверждённое предложение СБИС не найдено"})
			return
		}
		if err != nil {
			adminAPI.failed(response, "download manual Saby price XLSX", err)
			return
		}
		response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		response.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(content)
	}
}

func manualPriceProposalsHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok {
			return
		}
		id, ok := pimPathID(response, request, "variantId")
		if !ok {
			return
		}
		repository, ok := adminAPI.repository.(manualPriceProposalRepository)
		if !ok {
			adminAPI.failed(response, "manual price proposals unavailable", errors.New("manual price proposals unavailable"))
			return
		}
		items, err := repository.ListManualPriceProposals(request.Context(), id)
		if err != nil {
			adminAPI.failed(response, "list manual price proposals", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"proposals": items})
	}
}

func approveManualPriceProposalHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsManage)
		if !ok {
			return
		}
		id, ok := pimPathID(response, request, "proposalId")
		if !ok {
			return
		}
		repository, ok := adminAPI.repository.(manualPriceProposalRepository)
		if !ok {
			adminAPI.failed(response, "manual price proposals unavailable", errors.New("manual price proposals unavailable"))
			return
		}
		item, err := repository.ApproveManualPriceProposal(request.Context(), actor, id)
		if errors.Is(err, admin.ErrPriceChannelUnavailable) {
			writeJSON(response, http.StatusConflict, errorResponse{Error: "Канал изменения цены не подключён"})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(response, http.StatusConflict, errorResponse{Error: "Предложение уже подтверждено или устарело"})
			return
		}
		if err != nil {
			adminAPI.failed(response, "approve manual price proposal", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"proposal": item})
	}
}
