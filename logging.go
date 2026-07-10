package h2go

import (
	"context"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var (
	jdbcPasswordPattern  = regexp.MustCompile(`(?i)(;PASSWORD=)[^;]*`)
	urlUserInfoPattern   = regexp.MustCompile(`(?i)(h2(?:\+tcp)?://[^/@:]+:)[^@/]*(@)`)
	queryPasswordPattern = regexp.MustCompile(`(?i)([?&](?:password|passwd|pass|pwd)=)[^&]*`)
)

// NewTextLogger returns a diagnostic logger backed by slog's text handler.
//
// This is the recommended default logger constructor for h2go diagnostics,
// matching PRD requirements to use text output (not JSON).
//
// Example:
//
//	logger := h2go.NewTextLogger(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
//	cfg.Logger = logger
func NewTextLogger(w io.Writer, opts *slog.HandlerOptions) *slog.Logger {
	if w == nil {
		w = io.Discard
	}
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

// logConfig emits an optional diagnostic record for cfg.
//
// It returns true when a record was emitted, false when diagnostics are
// disabled (cfg == nil or cfg.Logger == nil).
//
// Sensitive keys (passwords and hashes) are redacted. String/error payloads are
// also scrubbed for common DSN password patterns.
func logConfig(cfg *Config, level slog.Level, msg string, attrs ...slog.Attr) bool {
	if cfg == nil || cfg.Logger == nil {
		return false
	}

	safe := make([]slog.Attr, 0, len(attrs)+3)
	safe = append(safe,
		slog.String("target", serverTarget(cfg)),
		slog.Int("protocol_expected", int(TCPProtocolVersion21)),
	)
	if cfg.Database != "" {
		safe = append(safe, slog.String("database", cfg.Database))
	}
	for _, attr := range attrs {
		safe = append(safe, sanitizeAttr(attr))
	}

	cfg.Logger.LogAttrs(context.Background(), level, msg, safe...)
	return true
}

func serverTarget(cfg *Config) string {
	if cfg == nil {
		return "?:?"
	}
	host := cfg.Host
	if host == "" {
		host = "?"
	}
	port := cfg.Port
	if port == "" {
		port = DefaultTCPPortStr
	}
	return net.JoinHostPort(host, port)
}

func sanitizeAttr(attr slog.Attr) slog.Attr {
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, redactedValue)
	}

	switch attr.Value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, redactSensitiveString(attr.Value.String()))
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			return slog.String(attr.Key, redactSensitiveString(err.Error()))
		}
	}
	return attr
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "password") || strings.Contains(k, "passwd") ||
		strings.Contains(k, "pwd") || strings.Contains(k, "hash")
}

func redactSensitiveString(s string) string {
	if s == "" {
		return s
	}
	s = jdbcPasswordPattern.ReplaceAllString(s, `${1}`+redactedValue)
	s = urlUserInfoPattern.ReplaceAllString(s, `${1}`+redactedValue+`${2}`)
	s = queryPasswordPattern.ReplaceAllString(s, `${1}`+redactedValue)
	return s
}
