package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

const testToken = "123456789:AAH-fake_Token-Value0123456789abcdef"

func TestRedactBotToken(t *testing.T) {
	in := "Post \"https://api.telegram.org/bot" + testToken + "/sendMessage\": connection reset by peer"
	got := redactBotToken(in)
	if strings.Contains(got, testToken) || strings.Contains(got, "AAH-fake") {
		t.Fatalf("token survived redaction: %q", got)
	}
	if !strings.Contains(got, "/bot<redacted>/sendMessage") {
		t.Fatalf("unexpected redaction output: %q", got)
	}
}

func TestLogHandlerRedactsTokenInErrorsAndStrings(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf, slog.LevelInfo))

	urlErr := &url.Error{Op: "Post", URL: "https://api.telegram.org/bot" + testToken + "/sendMessage", Err: errors.New("read: connection reset by peer")}
	logger.Error("send failed", "error", urlErr, "wrapped", fmt.Errorf("outer: %w", urlErr), "raw", "bot"+testToken)
	logger.With("error", urlErr).WithGroup("g").Info("grouped", "error", urlErr)

	out := buf.String()
	if strings.Contains(out, testToken) {
		t.Fatalf("token leaked into log output:\n%s", out)
	}
	if strings.Count(out, "bot<redacted>") < 5 {
		t.Fatalf("expected every attribute to be redacted, got:\n%s", out)
	}
	if !strings.Contains(out, "connection reset by peer") {
		t.Fatalf("redaction dropped the error detail:\n%s", out)
	}
}
