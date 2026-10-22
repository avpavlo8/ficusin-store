package mail

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestOrderEmailContainsBrandedHTMLAndPlainText(t *testing.T) {
	order := sampleOrder()
	order.CustomerName = "Александр"
	order.TrackNumber = "10338612280"
	letter := Confirmation(order)
	letter.To = "owner@example.invalid"
	sender := NewSender(Config{From: "info@ficusin.ru"}, nil)
	message, err := mail.ReadMessage(strings.NewReader(sender.compose(letter)))
	if err != nil {
		t.Fatal(err)
	}
	kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" {
		t.Fatalf("kind=%s err=%v", kind, err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	for _, expected := range []string{"text/plain", "text/html"} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(part.Header.Get("Content-Type"), expected) {
			t.Fatalf("unexpected part %v", part.Header)
		}
		// NextPart already decodes quoted printable.
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, value := range []string{"Здравствуйте, Александр!", "10338612280", "https://www.cdek.ru/ru/tracking?order_id=10338612280", "+7 915 615-11-00", "Новосёлов, 40А", "info@ficusin.ru"} {
			if !strings.Contains(text, value) {
				t.Fatalf("%s missing %q", expected, value)
			}
		}
		if expected == "text/html" && !strings.Contains(text, "/assets/brand/graphite-v1/wordmark-graphite.png") {
			t.Fatal("approved logo missing")
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("unexpected extra part: %v", err)
	}
}
func TestEmailEscapesCustomerContentAndURLs(t *testing.T) {
	order := sampleOrder()
	order.CustomerName = `<img src=x onerror=alert(1)>`
	order.Items[0].Name = `<script>bad</script>`
	order.PayURL = `https://example.invalid/pay?a=1&b="onclick=x`
	text := HTML(Confirmation(order))
	if strings.Contains(text, "<script>") || strings.Contains(text, "<img src=x") || strings.Contains(text, `b="onclick=`) {
		t.Fatal("customer text entered HTML unescaped")
	}
	if !strings.Contains(text, "&lt;script&gt;") {
		t.Fatal("escaped content missing")
	}
}
func TestTrackingRegistrationIsNotAShipmentNotice(t *testing.T) {
	order := sampleOrder()
	order.TrackNumber = "123"
	text := Tracking(order).Body
	if !strings.Contains(text, "ещё не означает передачу") || !strings.Contains(text, "order_id=123") {
		t.Fatal(text)
	}
}
