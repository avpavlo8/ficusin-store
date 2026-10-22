// Package mail sends the letters the shop owes its customers: the order
// they placed, what happened to it, and the invoice a company asked for.
//
// Everything here is switched off by an empty SMTP host, like every other
// integration in this shop. A shop with no mail settings simply sends no
// letters; nothing else changes.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	// From is the visible sender. It must be a mailbox on our own domain:
	// anything else is rejected by the receiving side as forgery.
	From     string
	FromName string
}

type Sender struct {
	config Config
	logger *slog.Logger
}

func NewSender(config Config, logger *slog.Logger) *Sender {
	config.Host = strings.TrimSpace(config.Host)
	config.From = strings.TrimSpace(config.From)
	if config.FromName == "" {
		config.FromName = "Фикусин"
	}
	if config.Port == 0 {
		config.Port = 465
	}
	return &Sender{config: config, logger: logger}
}

func (sender *Sender) Configured() bool {
	return sender != nil && sender.config.Host != "" && sender.config.From != ""
}

// Letter stores the durable plain-text message. SMTP sends both text and HTML.
type Letter struct {
	To      string
	Subject string
	Body    string
}

func (sender *Sender) Send(ctx context.Context, letter Letter) error {
	if !sender.Configured() {
		return errors.New("почта не настроена")
	}
	to := strings.TrimSpace(letter.To)
	if to == "" || !strings.Contains(to, "@") {
		return errors.New("некорректный адрес получателя")
	}
	address := net.JoinHostPort(sender.config.Host, fmt.Sprint(sender.config.Port))
	// The transport owns the connection: cancellation closes a stalled SMTP
	// exchange instead of leaving a SendMail goroutine behind after timeout.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := dialSMTP(ctx, address, sender.config.Port == 465, &tls.Config{
		ServerName: sender.config.Host, MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("соединение с почтой не удалось: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, sender.config.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if sender.config.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("почтовый сервер не поддерживает защищённое соединение STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: sender.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	auth := smtp.PlainAuth("", sender.config.Username, sender.config.Password, sender.config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("авторизация почты не удалась: %w", err)
	}
	if err := client.Mail(sender.config.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(sender.compose(letter))); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	// A successful DATA reply means the server accepted the letter. A failed
	// QUIT after that must not retry the already accepted message.
	_ = client.Quit()
	return nil
}

func dialSMTP(ctx context.Context, address string, implicitTLS bool, config *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{}
	if implicitTLS {
		return (&tls.Dialer{NetDialer: dialer, Config: config}).DialContext(ctx, "tcp", address)
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func (sender *Sender) compose(letter Letter) string {
	headers := []string{
		"From: " + mime.QEncoding.Encode("utf-8", sender.config.FromName) +
			" <" + sender.config.From + ">",
		"To: " + letter.To,
		"Reply-To: info@ficusin.ru",
		"Subject: " + mime.QEncoding.Encode("utf-8", letter.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
	}
	var body bytes.Buffer
	alternative := multipart.NewWriter(&body)
	for _, part := range []struct{ kind, body string }{{"text/plain", letter.Body}, {"text/html", HTML(letter)}} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", part.kind+"; charset=utf-8")
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		writer, _ := alternative.CreatePart(header)
		encoded := quotedprintable.NewWriter(writer)
		_, _ = encoded.Write([]byte(part.body))
		_ = encoded.Close()
	}
	_ = alternative.Close()
	headers = append(headers, `Content-Type: multipart/alternative; boundary="`+alternative.Boundary()+`"`)
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String()
}
