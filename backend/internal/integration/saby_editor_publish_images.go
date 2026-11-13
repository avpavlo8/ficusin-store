package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const sabyEditorMaxImageBytes = 10 << 20

// The image downloader never carries the Saby session. DNS is resolved and
// checked at dial time, and the selected IP is pinned for that connection.
func newSabyEditorImageClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("Небезопасный адрес фотографии")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("Не удалось определить адрес фотографии")
		}
		if len(ips) == 0 {
			return nil, errors.New("Адрес фотографии не найден")
		}
		for _, ip := range ips {
			if !sabyEditorPublicIP(ip.IP) {
				return nil, errors.New("Внутренний адрес фотографии запрещён")
			}
		}
		var last error
		for _, ip := range ips {
			conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("Слишком много перенаправлений фотографии")
		}
		return validateSabyEditorImageURL(req.URL)
	}}
}
func sabyEditorPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	// Shared-address space and documentation/benchmark networks are not public
	// media sources even though Go's IsGlobalUnicast intentionally includes them.
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
func validateSabyEditorImageURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return errors.New("Фотография должна иметь публичный HTTPS-адрес")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return errors.New("Локальные фотографии недоступны СБИС")
	}
	if ip := net.ParseIP(host); ip != nil && !sabyEditorPublicIP(ip) {
		return errors.New("Внутренний адрес фотографии запрещён")
	}
	return nil
}
func downloadSabyEditorImage(ctx context.Context, client *http.Client, source string) ([]byte, string, error) {
	u, err := url.Parse(source)
	if err != nil || validateSabyEditorImageURL(u) != nil {
		return nil, "", errors.New("Некорректный адрес фотографии для СБИС")
	}
	if client == nil {
		client = newSabyEditorImageClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp")
	response, err := client.Do(req)
	if err != nil {
		return nil, "", errors.New("Не удалось скачать фотографию для СБИС")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Источник фотографии вернул HTTP %d", response.StatusCode)
	}
	if response.ContentLength > sabyEditorMaxImageBytes {
		return nil, "", errors.New("Фотография для СБИС больше 10 МБ")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, sabyEditorMaxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > sabyEditorMaxImageBytes {
		return nil, "", errors.New("Не удалось прочитать фотографию для СБИС")
	}
	mime := http.DetectContentType(data)
	extension := ""
	switch mime {
	case "image/jpeg":
		extension = "jpg"
	case "image/png":
		extension = "png"
	case "image/webp":
		extension = "webp"
	default:
		return nil, "", errors.New("Для СБИС нужна фотография JPEG, PNG или WebP")
	}
	return data, extension, nil
}

// The JSON/base64 desktop path is vendor code, not an inferred upload format:
// WNCCore/Uploaders/ImageUploader + SbisFile/Source/Provider/BL._prepareRPCFile.
func (c *SabyClient) uploadSabyEditorImage(ctx context.Context, id int64, data []byte, extension string) (*sabyEditorNativeRecord, error) {
	if id <= 0 || len(data) == 0 || len(data) > sabyEditorMaxImageBytes {
		return nil, errors.New("Некорректная фотография СБИС")
	}
	if extension != "jpg" && extension != "png" && extension != "webp" {
		return nil, errors.New("Неизвестный формат фотографии СБИС")
	}
	var raw json.RawMessage
	params := map[string]any{"ИдО": id, "Файл": map[string]any{"Данные": base64.StdEncoding.EncodeToString(data), "ИмяФайла": "ficusin-" + strconv.FormatInt(id, 10) + "." + extension}}
	if err := c.sabyEditorWriteRPCOnce(ctx, "NomenclatureImages.UploadTemporary", params, &raw); err != nil {
		return nil, err
	}
	record, err := parseSabyEditorSingleNativeRecord(raw)
	if err != nil {
		return nil, err
	}
	uuid, _ := record.scalarString("UUID")
	if uuid == "" {
		uuid, _ = record.scalarString("md")
	}
	if uuid == "" {
		return nil, errors.New("СБИС не вернул идентификатор загруженной фотографии")
	}
	return record, nil
}
