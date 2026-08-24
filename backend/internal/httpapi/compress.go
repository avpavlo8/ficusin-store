package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// gzipResponses keeps the single-container deployment fast without relying on
// the platform proxy to compress large catalogue JSON and frontend bundles.
// Images, range requests and already encoded responses pass through unchanged.
func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead || request.Header.Get("Range") != "" || !acceptsGzip(request.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(response, request)
			return
		}
		writer := &gzipResponseWriter{ResponseWriter: response}
		defer writer.Close()
		next.ServeHTTP(writer, request)
	})
}

func acceptsGzip(header string) bool {
	for _, value := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(value), ";")
		disabled := false
		for _, parameter := range parts[1:] {
			if strings.TrimSpace(parameter) == "q=0" {
				disabled = true
			}
		}
		if strings.EqualFold(parts[0], "gzip") && !disabled {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
	status int
}

func (writer *gzipResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	contentType := writer.Header().Get("Content-Type")
	if status != http.StatusNoContent && status != http.StatusNotModified && compressibleContentType(contentType) && writer.Header().Get("Content-Encoding") == "" {
		writer.Header().Del("Content-Length")
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Add("Vary", "Accept-Encoding")
		writer.writer = gzip.NewWriter(writer.ResponseWriter)
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *gzipResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		if writer.Header().Get("Content-Type") == "" {
			writer.Header().Set("Content-Type", http.DetectContentType(body))
		}
		writer.WriteHeader(http.StatusOK)
	}
	if writer.writer != nil {
		return writer.writer.Write(body)
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *gzipResponseWriter) Close() {
	if writer.writer != nil {
		_ = writer.writer.Close()
	}
}

func (writer *gzipResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.writer != nil {
		return io.Copy(writer.writer, source)
	}
	return io.Copy(writer.ResponseWriter, source)
}

func compressibleContentType(value string) bool {
	value = strings.ToLower(value)
	return strings.HasPrefix(value, "text/") || strings.Contains(value, "json") || strings.Contains(value, "javascript") || strings.Contains(value, "svg") || strings.Contains(value, "xml")
}
