package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Sender is the interface for sending emails.
type Sender interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
}

// ---------------------------------------------------------------------------
// Resend implementation (https://resend.com)
// Free tier: 3,000 emails/month — best balance of simplicity and cost.
// ---------------------------------------------------------------------------

// ResendSender sends emails via the Resend API.
type ResendSender struct {
	apiKey      string
	fromAddress string
	fromName    string
	httpClient  *http.Client
	logger      zerolog.Logger
}

// NewResendSender creates a Resend email sender.
func NewResendSender(apiKey, fromAddress, fromName string, logger zerolog.Logger) *ResendSender {
	return &ResendSender{
		apiKey:      apiKey,
		fromAddress: fromAddress,
		fromName:    fromName,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      logger.With().Str("component", "email-resend").Logger(),
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

type resendResponse struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

func (r *ResendSender) Send(ctx context.Context, to, subject, htmlBody string) error {
	payload := resendRequest{
		From:    fmt.Sprintf("%s <%s>", r.fromName, r.fromAddress),
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		var resErr resendResponse
		_ = json.Unmarshal(respBody, &resErr)
		return fmt.Errorf("resend API error (status %d): %s", resp.StatusCode, resErr.Error)
	}

	var result resendResponse
	_ = json.Unmarshal(respBody, &result)
	r.logger.Debug().Str("email_id", result.ID).Str("to", to).Msg("Email sent via Resend")

	return nil
}

// ---------------------------------------------------------------------------
// SMTP implementation (free with any SMTP provider — Gmail, Mailgun, etc.)
// ---------------------------------------------------------------------------

// SMTPSender sends emails via plain SMTP.
type SMTPSender struct {
	host     string // e.g. "smtp.gmail.com"
	port     string // e.g. "587"
	username string
	password string
	from     string
	fromName string
	logger   zerolog.Logger
}

// NewSMTPSender creates an SMTP email sender.
func NewSMTPSender(host, port, username, password, from, fromName string, logger zerolog.Logger) *SMTPSender {
	return &SMTPSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		fromName: fromName,
		logger:   logger.With().Str("component", "email-smtp").Logger(),
	}
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, htmlBody string) error {
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", s.fromName, s.from)
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	addr := s.host + ":" + s.port

	err := smtp.SendMail(addr, auth, s.from, []string{to}, []byte(msg.String()))
	if err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}

	s.logger.Debug().Str("to", to).Msg("Email sent via SMTP")
	return nil
}

// ---------------------------------------------------------------------------
// Log-only sender (for development — no real emails)
// ---------------------------------------------------------------------------

// LogSender "sends" emails by logging them. Used in development.
type LogSender struct {
	logger zerolog.Logger
}

func NewLogSender(logger zerolog.Logger) *LogSender {
	return &LogSender{logger: logger.With().Str("component", "email-log").Logger()}
}

func (l *LogSender) Send(ctx context.Context, to, subject, htmlBody string) error {
	l.logger.Info().
		Str("to", to).
		Str("subject", subject).
		Int("html_length", len(htmlBody)).
		Msg("[DEV] Would send email")
	return nil
}
