// connector_test.go — tests for connector-level behaviour, including the
// ignored-DSN-parameters debug log (MATURITY_ROUND_II_PLAN.md Task 6,
// finding 7).

package h2go

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestLogIgnoredParamsKeysOnly verifies that the ignored-parameters debug
// record lists parameter KEYS and never their values (values may contain
// credentials).
func TestLogIgnoredParamsKeysOnly(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := &Config{
		Logger: logger,
		Params: map[string]string{
			"IFEXISTS":    "TRUE",
			"AUTO_SERVER": "TRUE",
			"PASSWORD":    "s3cret-value",
		},
	}

	if !logIgnoredParams(cfg) {
		t.Fatal("expected a log record to be emitted")
	}

	out := buf.String()
	for _, key := range []string{"IFEXISTS", "AUTO_SERVER", "PASSWORD"} {
		if !strings.Contains(out, key) {
			t.Errorf("log output should list key %q:\n%s", key, out)
		}
	}
	// Values must never appear.
	if strings.Contains(out, "s3cret-value") {
		t.Errorf("log output leaks a parameter value:\n%s", out)
	}
	if strings.Contains(out, "IFEXISTS=TRUE") || strings.Contains(out, "AUTO_SERVER=TRUE") {
		t.Errorf("log output contains key=value pairs; keys only expected:\n%s", out)
	}
	if !strings.Contains(out, "Level(4)") && !strings.Contains(out, "DEBUG") {
		t.Errorf("expected a debug-level record, got:\n%s", out)
	}
}

// TestLogIgnoredParamsQuietWithoutLoggerOrParams pins the quiet paths: no
// logger or no params means no emission and no panic.
func TestLogIgnoredParamsQuietWithoutLoggerOrParams(t *testing.T) {
	if logIgnoredParams(nil) {
		t.Error("nil config should not emit")
	}
	if logIgnoredParams(&Config{}) {
		t.Error("nil logger should not emit")
	}
	var buf bytes.Buffer
	cfg := &Config{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	if logIgnoredParams(cfg) {
		t.Error("empty params should not emit")
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected output: %q", buf.String())
	}
}
