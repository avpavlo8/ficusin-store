package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSabyEditorImageURLRejectsInternalDestinations(t *testing.T) {
	for _, raw := range []string{"http://example.com/a.jpg", "https://127.0.0.1/a", "https://user:secret@example.com/a", "https://10.0.0.2/a", "https://[::1]/a", "https://100.64.0.1/a", "https://localhost/a", "https://x.local/a", "https://169.254.169.254/a", "https://example.com:8443/a"} {
		u, _ := url.Parse(raw)
		if validateSabyEditorImageURL(u) == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	u, _ := url.Parse("https://s3.twcstorage.ru/ficusin-photos/a.jpg")
	if err := validateSabyEditorImageURL(u); err != nil {
		t.Fatal(err)
	}
}
func TestSabyEditorImageUploadUsesVendorDesktopBase64Contract(t *testing.T) {
	image := []byte("fake jpeg bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		var req struct {
			Method string
			Params map[string]any
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "NomenclatureImages.UploadTemporary" || req.Params["ИдО"] != float64(1101) {
			t.Errorf("unexpected operation: %+v", req)
		}
		file, _ := req.Params["Файл"].(map[string]any)
		if file["ИмяФайла"] != "ficusin-1101.jpg" || file["Данные"] != base64.StdEncoding.EncodeToString(image) {
			t.Error("upload contract mismatch")
		}
		if strings.Contains(file["Данные"].(string), "data:") {
			t.Error("unexpected dataURL header")
		}
		io.WriteString(w, `{"result":{"_type":"recordset","s":[{"n":"UUID","t":"UUID"},{"n":"UrlRelative","t":"Строка"}],"d":[["fixture-upload-id","/fixture-image"]]}}`)
	}))
	defer server.Close()
	c := NewSabyClient("id", "secret", "key", 278, 6)
	c.authURL = server.URL + "/auth"
	c.serviceURL = server.URL + "/rpc"
	result, err := c.uploadSabyEditorImage(context.Background(), 1101, image, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := result.scalarString("UUID"); id != "fixture-upload-id" {
		t.Fatalf("unexpected result: %s", id)
	}
}
func TestSabyEditorImageSingleRowResultDoesNotSilentlySelectFirst(t *testing.T) {
	raw := []byte(`{"_type":"recordset","s":[{"n":"UUID","t":"UUID"}],"d":[["a"],["b"]]}`)
	if _, err := parseSabyEditorSingleNativeRecord(raw); err == nil {
		t.Fatal("ambiguous uploads accepted")
	}
	// Reuse a real byte signature; header alone is intentionally ignored.
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	if http.DetectContentType(png) != "image/png" {
		t.Fatal("invalid fixture")
	}
}

func TestSabyEditorPublishingWriteDoesNotReplayAfterUnauthorized(t *testing.T) {
	requests := 0
	auths := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			auths++
			io.WriteString(w, `{"token":"fixture"}`)
			return
		}
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"expired"}`)
	}))
	defer server.Close()
	c := NewSabyClient("id", "secret", "key", 278, 6)
	c.authURL = server.URL + "/auth"
	c.serviceURL = server.URL + "/service/"
	if err := c.sabyEditorWriteRPCOnce(context.Background(), "Nomenclature.Create", map[string]any{}, nil); err == nil {
		t.Fatal("unauthorized write reported success")
	}
	if requests != 1 || auths != 1 {
		t.Fatalf("write replayed: writes=%d auths=%d", requests, auths)
	}
	if err := c.sabyEditorWriteRPCOnce(context.Background(), "Unknown.Write", nil, nil); err == nil || requests != 1 {
		t.Fatal("unsupported method executed")
	}
}
