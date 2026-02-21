package email

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogSender(t *testing.T) {
	logger := zerolog.Nop()
	sender := NewLogSender(logger)

	err := sender.Send(context.Background(), "test@example.com", "Test Subject", "<h1>Hello</h1>")
	if err != nil {
		t.Errorf("LogSender.Send: %v", err)
	}
}
