package httpapi

import "net/http"

func registerAdminCatalogToolRoutes(mux *http.ServeMux, adminAPI adminHandlers, storage productPhotoStorage) {
	mux.HandleFunc("GET /api/v1/admin/products/{id}/media", listProductMediaHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/products/{id}/media", uploadProductMediaHandler(adminAPI, storage))
	mux.HandleFunc("DELETE /api/v1/admin/products/{id}/media/{mediaId}", deleteProductMediaHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/products/{id}/media/{mediaId}/primary", primaryProductMediaHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/variants/{id}/media", listVariantMediaHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/variants/{id}/media", uploadVariantMediaHandler(adminAPI, storage))
	mux.HandleFunc("DELETE /api/v1/admin/variants/{id}/media/{mediaId}", deleteVariantMediaHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/variants/{id}/media/{mediaId}/primary", primaryVariantMediaHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/attributes", attributeDefinitionsHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/attributes", attributeDefinitionsHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/attributes/{attributeId}", attributeDefinitionHandler(adminAPI))
	mux.HandleFunc("DELETE /api/v1/admin/attributes/{attributeId}", attributeDefinitionHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/categories/{id}/effective-attributes", effectiveCategoryAttributesHandler(adminAPI))
	mux.HandleFunc("PUT /api/v1/admin/categories/{id}/attributes/{attributeId}", categoryAttributeAssignmentHandler(adminAPI))
	mux.HandleFunc("DELETE /api/v1/admin/categories/{id}/attributes/{attributeId}", categoryAttributeAssignmentHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/products/{id}/variants", productVariantsHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/products/{id}/variants", productVariantsHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/variants/{variantId}", productVariantHandler(adminAPI))
	mux.HandleFunc("DELETE /api/v1/admin/variants/{variantId}", productVariantHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/variants/{variantId}/copy", copyProductVariantHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/variants/{variantId}/archive", archiveProductVariantHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/catalog-filters", catalogFiltersHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/catalog-filters", catalogFiltersHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/catalog-filters/{filterId}", catalogFilterHandler(adminAPI))
	mux.HandleFunc("DELETE /api/v1/admin/catalog-filters/{filterId}", catalogFilterHandler(adminAPI))

	mux.HandleFunc("GET /api/v1/admin/collection-definitions", collectionDefinitionsHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/collection-definitions", collectionDefinitionsHandler(adminAPI))
	mux.HandleFunc("PUT /api/v1/admin/collection-definitions/{id}", collectionDefinitionHandler(adminAPI))
	mux.HandleFunc("DELETE /api/v1/admin/collection-definitions/{id}", collectionDefinitionHandler(adminAPI))
}
