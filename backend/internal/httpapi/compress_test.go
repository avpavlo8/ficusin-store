package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipResponsesCompressesJSON(t *testing.T) {
	handler := gzipResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = response.Write([]byte(`{"products":["` + strings.Repeat("plant", 200) + `"]}`))
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	request.Header.Set("Accept-Encoding", "br, gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "gzip" { t.Fatalf("Content-Encoding = %q", recorder.Header().Get("Content-Encoding")) }
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil { t.Fatal(err) }
	body, err := io.ReadAll(reader)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(body), "products") { t.Fatalf("unexpected body %q", body) }
}

func TestGzipResponsesLeavesImagesAndRangesAlone(t *testing.T) {
	for _, test := range []struct{name, contentType, rangeHeader string}{
		{name:"image",contentType:"image/webp"}, {name:"range",contentType:"text/javascript",rangeHeader:"bytes=0-20"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := gzipResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.Header().Set("Content-Type", test.contentType); _, _ = response.Write([]byte("content")) }))
			request := httptest.NewRequest(http.MethodGet, "/asset", nil); request.Header.Set("Accept-Encoding", "gzip"); request.Header.Set("Range", test.rangeHeader)
			recorder := httptest.NewRecorder(); handler.ServeHTTP(recorder, request)
			if recorder.Header().Get("Content-Encoding") != "" { t.Fatalf("unexpected gzip for %s", test.name) }
		})
	}
}
