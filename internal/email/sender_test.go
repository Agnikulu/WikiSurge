package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Sender interface satisfaction
// ---------------------------------------------------------------------------

func TestResendSenderImplementsSender(t *testing.T) {
	var _ Sender = &ResendSender{}
}

func TestSMTPSenderImplementsSender(t *testing.T) {
	var _ Sender = &SMTPSender{}
}

func TestLogSenderImplementsSender(t *testing.T) {
	var _ Sender = &LogSender{}
}

// ---------------------------------------------------------------------------
// NewResendSender
// ---------------------------------------------------------------------------

func TestNewResendSender(t *testing.T) {
	s := NewResendSender("re_abc123", "noreply@example.com", "WikiSurge", zerolog.Nop())
	require.NotNil(t, s)
	assert.Equal(t, "re_abc123", s.apiKey)
	assert.Equal(t, "noreply@example.com", s.fromAddress)
	assert.Equal(t, "WikiSurge", s.fromName)
	assert.NotNil(t, s.httpClient)
}

// ---------------------------------------------------------------------------
// ResendSender.Send — success
// ---------------------------------------------------------------------------

func TestResendSend_Success(t *testing.T) {
	var captured resendRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		json.NewDecoder(r.Body).Decode(&captured)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resendResponse{ID: "email-123"})
	}))
	defer ts.Close()

	s := NewResendSender("test-key", "from@example.com", "Test", zerolog.Nop())
	// Override the HTTP client to talk to our test server
	s.httpClient = ts.Client()

	// We need to override the URL. Since Send hard-codes the URL, we'll create a
	// custom test that exercises the request building and parsing logic by using
	// a transport that redirects to our test server.
	transport := &rewriteTransport{base: ts.Client().Transport, url: ts.URL + "/emails"}
	s.httpClient.Transport = transport

	err := s.Send(context.Background(), "recipient@example.com", "Hello", "<p>Body</p>")
	require.NoError(t, err)

	assert.Equal(t, []string{"recipient@example.com"}, captured.To)
	assert.Equal(t, "Hello", captured.Subject)
	assert.Equal(t, "<p>Body</p>", captured.HTML)
	assert.Contains(t, captured.From, "from@example.com")
}

// rewriteTransport redirects all requests to a fixed URL (for httptest).
type rewriteTransport struct {
	base http.RoundTripper
	url  string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())
	parsed, _ := http.NewRequest(req.Method, t.url, req.Body)
	newReq.URL = parsed.URL
	newReq.Host = parsed.URL.Host
	if t.base != nil {
		return t.base.RoundTrip(newReq)
	}
	return http.DefaultTransport.RoundTrip(newReq)
}

// ---------------------------------------------------------------------------
// ResendSender.Send — API error
// ---------------------------------------------------------------------------

func TestResendSend_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(resendResponse{Error: "invalid recipient"})
	}))
	defer ts.Close()

	s := NewResendSender("key", "from@example.com", "Test", zerolog.Nop())
	s.httpClient.Transport = &rewriteTransport{url: ts.URL}

	err := s.Send(context.Background(), "bad@", "Sub", "<p>x</p>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resend API error")
	assert.Contains(t, err.Error(), "invalid recipient")
}

// ---------------------------------------------------------------------------
// ResendSender.Send — network error
// ---------------------------------------------------------------------------

func TestResendSend_NetworkError(t *testing.T) {
	s := NewResendSender("key", "from@example.com", "Test", zerolog.Nop())
	// Point to a closed server
	s.httpClient.Transport = &rewriteTransport{url: "http://127.0.0.1:1"}

	err := s.Send(context.Background(), "to@example.com", "Sub", "<p>x</p>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resend API call")
}

// ---------------------------------------------------------------------------
// ResendSender.Send — cancelled context
// ---------------------------------------------------------------------------

func TestResendSend_CancelledContext(t *testing.T) {
	s := NewResendSender("key", "from@example.com", "Test", zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Send(ctx, "to@example.com", "Sub", "<p>x</p>")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// NewSMTPSender
// ---------------------------------------------------------------------------

func TestNewSMTPSender(t *testing.T) {
	s := NewSMTPSender("smtp.gmail.com", "587", "user", "pass", "from@gmail.com", "WikiSurge", zerolog.Nop())
	require.NotNil(t, s)
	assert.Equal(t, "smtp.gmail.com", s.host)
	assert.Equal(t, "587", s.port)
	assert.Equal(t, "user", s.username)
	assert.Equal(t, "pass", s.password)
	assert.Equal(t, "from@gmail.com", s.from)
	assert.Equal(t, "WikiSurge", s.fromName)
}

// ---------------------------------------------------------------------------
// SMTPSender.Send — connection refused (no SMTP server)
// ---------------------------------------------------------------------------

func TestSMTPSend_ConnectionRefused(t *testing.T) {
	s := NewSMTPSender("127.0.0.1", "19999", "user", "pass", "from@test.com", "T", zerolog.Nop())
	err := s.Send(context.Background(), "to@test.com", "Sub", "<p>x</p>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "smtp send")
}

// ---------------------------------------------------------------------------
// LogSender
// ---------------------------------------------------------------------------

func TestLogSender_Send(t *testing.T) {
	s := NewLogSender(zerolog.Nop())
	err := s.Send(context.Background(), "to@example.com", "Subject", "<h1>Hi</h1>")
	require.NoError(t, err)
}

func TestLogSender_EmptyBody(t *testing.T) {
	s := NewLogSender(zerolog.Nop())
	err := s.Send(context.Background(), "to@example.com", "Sub", "")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// resendRequest serialisation
// ---------------------------------------------------------------------------

func TestResendRequest_JSON(t *testing.T) {
	r := resendRequest{
		From:    "Sender <s@e.com>",
		To:      []string{"a@b.com", "c@d.com"},
		Subject: "Hi",
		HTML:    "<b>Bold</b>",
	}
	data, err := json.Marshal(r)
	require.NoError(t, err)

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	assert.Equal(t, "Hi", decoded["subject"])
	assert.Equal(t, "<b>Bold</b>", decoded["html"])
}

func TestResendResponse_JSON(t *testing.T) {
	data := `{"id":"msg_123","error":"oops"}`
	var r resendResponse
	require.NoError(t, json.Unmarshal([]byte(data), &r))
	assert.Equal(t, "msg_123", r.ID)
	assert.Equal(t, "oops", r.Error)
}
