package h2go

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextLogger_ProducesText(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger.Debug("hello")
	out := buf.String()
	if out == "" {
		t.Fatal("expected log output")
	}
	if strings.Contains(out, "{\"") {
		t.Fatalf("expected text log output, got JSON-ish: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected message in output, got %q", out)
	}
}

func TestLogConfig_DisabledWhenNoLogger(t *testing.T) {
	cfg := &Config{Host: "localhost", Port: "9092", Database: "db"}
	emitted := logConfig(cfg, slog.LevelInfo, "msg")
	if emitted {
		t.Fatal("expected emitted=false when logger is nil")
	}
}

func TestLogConfig_RedactsSensitiveData(t *testing.T) {
	var buf bytes.Buffer
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "mydb",
		Logger:   NewTextLogger(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}

	secretURL := "h2://sa:secret@localhost:9092/mydb?password=12345"
	secretJDBC := "jdbc:h2:tcp://localhost:9092/mydb;USER=sa;PASSWORD=topsecret"
	secretErr := errors.New("dial failed for " + secretURL)
	secretHash := "0123456789abcdef"

	emitted := logConfig(cfg, slog.LevelError, "connect failed",
		slog.String("dsn", secretURL),
		slog.String("jdbc", secretJDBC),
		slog.String("password", "secret"),
		slog.String("password_hash", secretHash),
		slog.Any("error", secretErr),
	)
	if !emitted {
		t.Fatal("expected emitted=true")
	}

	out := buf.String()
	if out == "" {
		t.Fatal("expected log output")
	}

	for _, leak := range []string{"secret", "topsecret", "12345", secretHash} {
		if strings.Contains(out, leak) {
			t.Fatalf("output leaked sensitive value %q: %q", leak, out)
		}
	}

	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redaction marker in output, got %q", out)
	}
	if !strings.Contains(out, "target=") {
		t.Fatalf("expected target attribute in output, got %q", out)
	}
	if !strings.Contains(out, "protocol_expected=21") {
		t.Fatalf("expected protocol_expected attribute in output, got %q", out)
	}
}

func TestServerTarget_DefaultPort(t *testing.T) {
	cfg := &Config{Host: "example.com"}
	got := serverTarget(cfg)
	if got != "example.com:9092" {
		t.Fatalf("serverTarget() = %q, want %q", got, "example.com:9092")
	}
}

func TestRedactSensitiveString(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db;USER=sa;PASSWORD=abc h2://sa:pw@localhost:9092/db?password=xyz"
	got := redactSensitiveString(input)
	if strings.Contains(got, "PASSWORD=abc") || strings.Contains(got, ":pw@") || strings.Contains(got, "password=xyz") {
		t.Fatalf("redaction failed: %q", got)
	}
	if !strings.Contains(got, "PASSWORD=[REDACTED]") {
		t.Fatalf("expected JDBC password redaction, got %q", got)
	}
}
