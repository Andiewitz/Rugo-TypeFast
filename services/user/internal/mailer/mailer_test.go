package mailer

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
)

func TestMessageBodySanitizesSubject(t *testing.T) {
	t.Parallel()

	body := messageBody("no-reply@example.com", Message{
		To:      "user@example.com",
		Subject: "Hello\r\nBcc: attacker@example.com",
		Text:    "Welcome",
	})
	if strings.Contains(body, "\r\nBcc:") {
		t.Fatal("subject header injection was not removed")
	}
	if !strings.Contains(body, "Subject: HelloBcc: attacker@example.com") {
		t.Fatalf("unexpected body: %q", body)
	}
	if !strings.HasSuffix(body, "\r\n\r\nWelcome\r\n") {
		t.Fatalf("message body missing: %q", body)
	}
}

func TestSMTPRejectsUnknownTLSMode(t *testing.T) {
	t.Parallel()

	sender := &SMTP{Addr: "127.0.0.1:25", From: "no-reply@example.com", TLSMode: "maybe"}
	if err := sender.Send(context.Background(), Message{To: "user@example.com"}); err == nil {
		t.Fatal("expected an unknown TLS mode to fail")
	}
}

// A relay that cannot upgrade must be refused rather than silently downgraded:
// the message body carries verification and reset tokens.
func TestSMTPRefusesRelayWithoutSTARTTLS(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = connection.Write([]byte("220 localhost ESMTP\r\n"))
		reader := bufio.NewReader(connection)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				_, _ = connection.Write([]byte("250-localhost\r\n250 HELP\r\n"))
			case strings.HasPrefix(upper, "QUIT"):
				_, _ = connection.Write([]byte("221 bye\r\n"))
				return
			default:
				_, _ = connection.Write([]byte("500 unrecognized\r\n"))
			}
		}
	}()

	sender := &SMTP{Addr: listener.Addr().String(), From: "no-reply@example.com", TLSMode: TLSRequired}
	err = sender.Send(context.Background(), Message{To: "user@example.com"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected a STARTTLS error, got %v", err)
	}
}

func TestSMTPRejectsInvalidAddress(t *testing.T) {
	t.Parallel()

	sender := &SMTP{Addr: "not-an-address", From: "no-reply@example.com"}
	if err := sender.Send(context.Background(), Message{To: "user@example.com"}); err == nil {
		t.Fatal("expected invalid address error")
	}
}
