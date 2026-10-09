package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemovedDeliveryProviderEndpointsAreUnavailable(t *testing.T) {
	t.Parallel()
	router := NewRouter(discardLogger(), testDependencies(catalogStub{}, authStub{}))
	for _, path := range []string{"/api/v1/delivery/providers", "/api/v1/delivery/post", "/api/v1/delivery/courier"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s returned %d, want 404", path, response.Code)
		}
	}
}
