package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPImplicitTLSVerifiesCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dialSMTP(ctx, server.Listener.Addr().String(), true, &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if conn, err := dialSMTP(ctx, server.Listener.Addr().String(), true, &tls.Config{RootCAs: roots, ServerName: "wrong.example", MinVersion: tls.VersionTLS12}); err == nil {
		conn.Close()
		t.Fatal("wrong certificate hostname accepted")
	}
}

func TestSMTPRefusesUnencryptedAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "220 local SMTP\r\n")
		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n')
		if strings.HasPrefix(line, "EHLO") {
			fmt.Fprint(conn, "250-local\r\n250 AUTH PLAIN\r\n")
		}
		rest, _ := io.ReadAll(reader)
		commands <- string(rest)
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	sender := NewSender(Config{Host: "127.0.0.1", Port: n, Username: "robot@example.com", Password: "test-only", From: "robot@example.com"}, nil)
	err = sender.Send(context.Background(), Letter{To: "recipient@example.com", Body: "test"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected required TLS, got %v", err)
	}
	if command := <-commands; strings.Contains(command, "AUTH") {
		t.Fatal("credentials transmitted without TLS")
	}
}

func TestSMTPCancellationClosesStalledConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	closed := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
		close(closed)
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	sender := NewSender(Config{Host: "127.0.0.1", Port: n, From: "robot@example.com"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := sender.Send(ctx, Letter{To: "recipient@example.com"}); err == nil {
		t.Fatal("stalled server accepted")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection remained open after cancellation")
	}
}
