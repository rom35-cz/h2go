package h2go

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
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
	secretJDBC := "jdbc:h2:tcp://sa:jwtpass@localhost:9092/mydb;USER=sa;PASSWORD=topsecret"
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

	for _, leak := range []string{"secret", "topsecret", "jwtpass", "12345", secretHash} {
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
	cases := []struct {
		name  string
		input string
		leaks []string // substrings that must NOT appear in output
		wants []string // substrings that MUST appear in output
	}{
		{
			name:  "native userinfo",
			input: "h2://sa:pw@localhost:9092/db?password=xyz",
			leaks: []string{"pw", "xyz"},
			wants: []string{"[REDACTED]"},
		},
		{
			name:  "native+tcp userinfo",
			input: "h2+tcp://sa:pw@localhost:9092/db",
			leaks: []string{"pw"},
			wants: []string{"[REDACTED]"},
		},
		{
			name:  "jdbc userinfo (h2:tcp://user:pass@)",
			input: "jdbc:h2:tcp://sa:secret@localhost:9092/db",
			leaks: []string{"secret"},
			wants: []string{"[REDACTED]"},
		},
		{
			name:  "jdbc userinfo + semicolon password",
			input: "jdbc:h2:tcp://sa:secret@localhost:9092/db;USER=sa;PASSWORD=topsecret",
			leaks: []string{"secret", "topsecret"},
			wants: []string{"[REDACTED]"},
		},
		{
			name:  "jdbc semicolon password only",
			input: "jdbc:h2:tcp://localhost:9092/db;USER=sa;PASSWORD=abc",
			leaks: []string{"abc"},
			wants: []string{"PASSWORD=[REDACTED]"},
		},
		{
			name:  "empty password in userinfo",
			input: "h2://user:@localhost:9092/db",
			leaks: nil,
			wants: []string{"[REDACTED]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSensitiveString(tc.input)
			for _, leak := range tc.leaks {
				if strings.Contains(got, leak) {
					t.Fatalf("redaction leaked %q: %q", leak, got)
				}
			}
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q in output, got %q", want, got)
				}
			}
		})
	}
}

// TestLogConfig_NoDoubleLoggingOnHandshakeFailure verifies that a failed
// connection attempt through connector.Connect does not emit duplicate error
// records. The handshake defer logs the error once; connector.Connect must not
// log it again.
func TestLogConfig_NoDoubleLoggingOnHandshakeFailure(t *testing.T) {
	var buf bytes.Buffer
	cfg := &Config{
		Host:     "127.0.0.1",
		Port:     "1", // port 1 is reserved, dial will fail immediately
		Database: "testdb",
		Logger:   NewTextLogger(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}

	connr, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = connr.Connect(ctx)
	if err == nil {
		t.Fatal("expected connection error")
	}

	out := buf.String()
	// Count error-level records. Each error record contains level=ERROR.
	errorCount := strings.Count(out, "level=ERROR")
	if errorCount != 1 {
		t.Fatalf("expected exactly 1 error record, got %d:\n%s", errorCount, out)
	}
}
