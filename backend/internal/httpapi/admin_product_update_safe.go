package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

// safeAdminProductUpdateHandler keeps the legacy single-image field backward
// compatible without letting it destroy a multi-image gallery on every save.
// The old editor always sends its current primary image even when the operator
// only changed description. If that value is unchanged we remove just that
// no-op field and let the existing, fully validated update handler do the rest.
func safeAdminProductUpdateHandler(adminAPI adminHandlers) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := adminAPI.authorize(response, request, admin.PermissionProductsEdit)
		if !ok {
			return
		}
		productID, ok := pathID(response, request)
		if !ok {
			return
		}

		raw, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
			return
		}
		if encoded, present := fields["image"]; present {
			var incoming string
			if json.Unmarshal(encoded, &incoming) == nil {
				products, listErr := adminAPI.repository.ListProducts(request.Context())
				if listErr != nil {
					adminAPI.failed(response, "read product before update", listErr)
					return
				}
				for _, product := range products {
					if product.ID == productID && strings.TrimSpace(product.Image) == strings.TrimSpace(incoming) {
						delete(fields, "image")
						break
					}
				}
			}
		}
		raw, err = json.Marshal(fields)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		request.ContentLength = int64(len(raw))
		adminAPI.updateProduct(response, request)
	}
}
