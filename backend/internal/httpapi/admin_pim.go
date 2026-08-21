package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type catalogPIMRepository interface {
	ListAttributeDefinitions(context.Context) ([]admin.AttributeDefinition, error)
	CreateAttributeDefinition(context.Context, admin.Actor, admin.AttributeDefinitionInput) (admin.AttributeDefinition, error)
	UpdateAttributeDefinition(context.Context, admin.Actor, int64, admin.AttributeDefinitionInput) (admin.AttributeDefinition, error)
	ArchiveAttributeDefinition(context.Context, admin.Actor, int64) error
	EffectiveCategoryAttributes(context.Context, int64) ([]admin.EffectiveCategoryAttribute, error)
	SetCategoryAttribute(context.Context, admin.Actor, int64, admin.CategoryAttributeInput) error
	RemoveCategoryAttribute(context.Context, admin.Actor, int64, int64) error
	ListProductVariants(context.Context, int64) ([]admin.AdminVariant, error)
	CreateProductVariant(context.Context, admin.Actor, int64, admin.VariantInput) (admin.AdminVariant, error)
	UpdateProductVariant(context.Context, admin.Actor, int64, admin.VariantInput) (admin.AdminVariant, error)
	CopyProductVariant(context.Context, admin.Actor, int64) (admin.AdminVariant, error)
	ArchiveProductVariant(context.Context, admin.Actor, int64) error
	DeleteProductVariant(context.Context, admin.Actor, int64) error
	ListCatalogFilters(context.Context) ([]admin.CatalogFilter, error)
	CreateCatalogFilter(context.Context, admin.Actor, admin.CatalogFilterInput) (admin.CatalogFilter, error)
	UpdateCatalogFilter(context.Context, admin.Actor, int64, admin.CatalogFilterInput) (admin.CatalogFilter, error)
	DeleteCatalogFilter(context.Context, admin.Actor, int64) error
}

func pimRepository(adminAPI adminHandlers, response http.ResponseWriter) (catalogPIMRepository, bool) {
	repository, ok := adminAPI.repository.(catalogPIMRepository)
	if !ok {
		adminAPI.failed(response, "catalog PIM unavailable", errors.New("catalog PIM unavailable"))
	}
	return repository, ok
}

func pimPathID(response http.ResponseWriter, request *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(request.PathValue(name)), 10, 64)
	if err != nil || value < 1 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный идентификатор"})
		return 0, false
	}
	return value, true
}

func attributeDefinitionsHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead)
		if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodGet {
			items, err := repository.ListAttributeDefinitions(request.Context())
			if err != nil { adminAPI.failed(response, "list attributes", err); return }
			writeJSON(response, http.StatusOK, map[string]any{"attributes": items})
			return
		}
		if actor.Role != admin.RoleOwner { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может менять структуру атрибутов"}); return }
		var input admin.AttributeDefinitionInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.CreateAttributeDefinition(request.Context(), actor, input)
		if err != nil { adminAPI.failed(response, "create attribute", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"attribute": item})
	}
}

func attributeDefinitionHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		if actor.Role != admin.RoleOwner { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может менять структуру атрибутов"}); return }
		id, ok := pimPathID(response, request, "attributeId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodDelete {
			if err := repository.ArchiveAttributeDefinition(request.Context(), actor, id); err != nil { adminAPI.failed(response, "archive attribute", err); return }
			writeJSON(response, http.StatusOK, map[string]bool{"archived": true}); return
		}
		var input admin.AttributeDefinitionInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.UpdateAttributeDefinition(request.Context(), actor, id, input)
		if err != nil { adminAPI.failed(response, "update attribute", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"attribute": item})
	}
}

func effectiveCategoryAttributesHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		categoryID, ok := pimPathID(response, request, "id"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		items, err := repository.EffectiveCategoryAttributes(request.Context(), categoryID)
		if err != nil { adminAPI.failed(response, "effective category attributes", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"attributes": items})
	}
}

func categoryAttributeAssignmentHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		if actor.Role != admin.RoleOwner { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может менять схему категории"}); return }
		categoryID, ok := pimPathID(response, request, "id"); if !ok { return }
		attributeID, ok := pimPathID(response, request, "attributeId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodDelete {
			if err := repository.RemoveCategoryAttribute(request.Context(), actor, categoryID, attributeID); err != nil { adminAPI.failed(response, "remove category attribute", err); return }
			writeJSON(response, http.StatusOK, map[string]bool{"deleted": true}); return
		}
		var input admin.CategoryAttributeInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		input.AttributeID = attributeID
		if err := repository.SetCategoryAttribute(request.Context(), actor, categoryID, input); err != nil { adminAPI.failed(response, "set category attribute", err); return }
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	}
}

func productVariantsHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		productID, ok := pimPathID(response, request, "id"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodGet {
			items, err := repository.ListProductVariants(request.Context(), productID)
			if err != nil { adminAPI.failed(response, "list product variants", err); return }
			writeJSON(response, http.StatusOK, map[string]any{"variants": items}); return
		}
		if !admin.Can(actor.Role, admin.PermissionProductsEdit) { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Недостаточно прав"}); return }
		var input admin.VariantInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.CreateProductVariant(request.Context(), actor, productID, input)
		if err != nil { adminAPI.failed(response, "create product variant", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"variant": item})
	}
}

func productVariantHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit); if !ok { return }
		variantID, ok := pimPathID(response, request, "variantId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodDelete {
			if err := repository.DeleteProductVariant(request.Context(), actor, variantID); err != nil { adminAPI.failed(response, "delete product variant", err); return }
			writeJSON(response, http.StatusOK, map[string]bool{"deleted": true}); return
		}
		var input admin.VariantInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.UpdateProductVariant(request.Context(), actor, variantID, input)
		if err != nil { adminAPI.failed(response, "update product variant", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"variant": item})
	}
}

func copyProductVariantHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit); if !ok { return }
		variantID, ok := pimPathID(response, request, "variantId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		item, err := repository.CopyProductVariant(request.Context(), actor, variantID)
		if err != nil { adminAPI.failed(response, "copy product variant", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"variant": item})
	}
}

func archiveProductVariantHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit); if !ok { return }
		variantID, ok := pimPathID(response, request, "variantId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if err := repository.ArchiveProductVariant(request.Context(), actor, variantID); err != nil { adminAPI.failed(response, "archive product variant", err); return }
		writeJSON(response, http.StatusOK, map[string]bool{"archived": true})
	}
}

func catalogFiltersHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodGet {
			items, err := repository.ListCatalogFilters(request.Context()); if err != nil { adminAPI.failed(response, "list catalog filters", err); return }
			writeJSON(response, http.StatusOK, map[string]any{"filters": items}); return
		}
		if actor.Role != admin.RoleOwner { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может менять фильтры"}); return }
		var input admin.CatalogFilterInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.CreateCatalogFilter(request.Context(), actor, input); if err != nil { adminAPI.failed(response, "create catalog filter", err); return }
		writeJSON(response, http.StatusCreated, map[string]any{"filter": item})
	}
}

func catalogFilterHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, actor, ok := adminAPI.authorize(response, request, admin.PermissionProductsRead); if !ok { return }
		if actor.Role != admin.RoleOwner { writeJSON(response, http.StatusForbidden, errorResponse{Error: "Только владелец может менять фильтры"}); return }
		id, ok := pimPathID(response, request, "filterId"); if !ok { return }
		repository, ok := pimRepository(adminAPI, response); if !ok { return }
		if request.Method == http.MethodDelete {
			if err := repository.DeleteCatalogFilter(request.Context(), actor, id); err != nil { adminAPI.failed(response, "delete catalog filter", err); return }
			writeJSON(response, http.StatusOK, map[string]bool{"deleted": true}); return
		}
		var input admin.CatalogFilterInput
		if decodeJSON(request, &input) != nil { writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"}); return }
		item, err := repository.UpdateCatalogFilter(request.Context(), actor, id, input); if err != nil { adminAPI.failed(response, "update catalog filter", err); return }
		writeJSON(response, http.StatusOK, map[string]any{"filter": item})
	}
}
