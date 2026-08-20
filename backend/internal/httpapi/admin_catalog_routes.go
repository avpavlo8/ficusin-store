package httpapi

import "net/http"

func registerAdminCatalogToolRoutes(mux *http.ServeMux, adminAPI adminHandlers, storage productPhotoStorage) {
	mux.HandleFunc("POST /api/v1/admin/products/import-all", importAllProductsHandler(adminAPI))
	mux.HandleFunc("GET /api/v1/admin/products/{id}/media", listProductMediaHandler(adminAPI))
	mux.HandleFunc("POST /api/v1/admin/products/{id}/media", uploadProductMediaHandler(adminAPI, storage))
	mux.HandleFunc("DELETE /api/v1/admin/products/{id}/media/{mediaId}", deleteProductMediaHandler(adminAPI))
	mux.HandleFunc("PATCH /api/v1/admin/products/{id}/media/{mediaId}/primary", primaryProductMediaHandler(adminAPI))
}
