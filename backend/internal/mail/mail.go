// Package mail sends the letters the shop owes its customers: the order
// they placed, what happened to it, and the invoice a company asked for.
//
// Everything here is switched off by an empty SMTP host, like every other
// integration in this shop. A shop with no mail settings simply sends no
// letters; nothing else changes.
package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/smtp"
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

// Letter is one message. Plain text only: a shop confirmation is read in two
// seconds, and HTML mail is where spam filters get suspicious.
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
	message := sender.compose(letter)
	address := fmt.Sprintf("%s:%d", sender.config.Host, sender.config.Port)
	auth := smtp.PlainAuth("", sender.config.Username, sender.config.Password, sender.config.Host)

	// A slow mail server must not hold up an order, so sending happens with
	// a deadline and its failure is never fatal to the caller.
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(address, auth, sender.config.From, []string{to}, []byte(message))
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("отправка письма не удалась: %w", err)
		}
		return nil
	}
}

func (sender *Sender) compose(letter Letter) string {
	headers := []string{
		"From: " + mime.QEncoding.Encode("utf-8", sender.config.FromName) +
			" <" + sender.config.From + ">",
		"To: " + letter.To,
		"Subject: " + mime.QEncoding.Encode("utf-8", letter.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + letter.Body + "\r\n"
}
