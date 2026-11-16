package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Transport security for SMTP. Verification and reset links are bearer tokens,
// so a clear-text hop lets a network attacker take over accounts.
const (
	TLSRequired      = "starttls"
	TLSOpportunistic = "opportunistic"
	TLSNone          = "none"
)

type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

type Mailer interface {
	Send(ctx context.Context, message Message) error
}

type Log struct {
	Logger *slog.Logger
}

func (l *Log) Send(_ context.Context, message Message) error {
	l.Logger.Info("email delivery skipped in log mode",
		"to", message.To,
		"subject", message.Subject,
	)
	return nil
}

type SMTP struct {
	Addr    string
	From    string
	TLSMode string
}

func normalizeTLSMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TLSRequired:
		return TLSRequired, nil
	case TLSOpportunistic:
		return TLSOpportunistic, nil
	case TLSNone:
		return TLSNone, nil
	default:
		return "", fmt.Errorf("unknown SMTP TLS mode %q", value)
	}
}

func (s *SMTP) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("invalid smtp address: %w", err)
	}

	mode, err := normalizeTLSMode(s.TLSMode)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	defer connection.Close()

	deadline := time.Now().Add(30 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)

	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if mode != TLSNone {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		} else if mode == TLSRequired {
			return fmt.Errorf("smtp server %s does not offer STARTTLS", host)
		}
	}

	if err := client.Mail(s.From); err != nil {
		return fmt.Errorf("smtp sender: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("smtp recipient: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(messageBody(s.From, message))); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp finish: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

func messageBody(from string, message Message) string {
	headers := []string{
		"From: " + from,
		"To: " + message.To,
		"Subject: " + sanitizeHeader(message.Subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + message.Text + "\r\n"
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}
