package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type sabyLinkRepository interface {
	SearchSabyLinkCandidates(context.Context, string) ([]admin.SabyLinkCandidate, error)
	LinkVariantToSaby(context.Context, admin.Actor, int64, string) error
}

func sabyLinkCandidatesHandler(handlers adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsRead)
		if !ok {
			return
		}
		if actor.Role != admin.RoleOwner {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может связывать товары со СБИС"})
			return
		}
		repository, ok := handlers.repository.(sabyLinkRepository)
		if !ok {
			handlers.failed(response, "Saby links unavailable", errors.New("Saby links unavailable"))
			return
		}
		items, err := repository.SearchSabyLinkCandidates(request.Context(), request.URL.Query().Get("q"))
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
				return
			}
			handlers.failed(response, "search Saby candidates", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"candidates": items})
	}
}

func linkVariantToSabyHandler(handlers adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsManage)
		if !ok {
			return
		}
		if actor.Role != admin.RoleOwner {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может связывать товары со СБИС"})
			return
		}
		variantID, ok := pimPathID(response, request, "variantId")
		if !ok {
			return
		}
		var input struct {
			SabyID string `json:"sabyId"`
		}
		if decodeJSON(request, &input) != nil || strings.TrimSpace(input.SabyID) == "" {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите запись СБИС"})
			return
		}
		repository, ok := handlers.repository.(sabyLinkRepository)
		if !ok {
			handlers.failed(response, "Saby links unavailable", errors.New("Saby links unavailable"))
			return
		}
		if err := repository.LinkVariantToSaby(request.Context(), actor, variantID, input.SabyID); err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
				return
			}
			handlers.failed(response, "link variant to Saby", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"linked": true})
	}
}
