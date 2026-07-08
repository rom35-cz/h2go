package h2go

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDSN_Basic(t *testing.T) {
	cfg, err := ParseDSN("jdbc:h2:tcp://localhost:9092/h2-go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != "9092" {
		t.Errorf("Port = %q, want 9092", cfg.Port)
	}
	if cfg.Database != "h2-go" {
		t.Errorf("Database = %q, want h2-go", cfg.Database)
	}
	if cfg.User != "" {
		t.Errorf("User = %q, want empty", cfg.User)
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
	if len(cfg.Params) != 0 {
		t.Errorf("Params = %v, want empty", cfg.Params)
	}
	if cfg.OriginalURL != "jdbc:h2:tcp://localhost:9092/h2-go" {
		t.Errorf("OriginalURL = %q, want input", cfg.OriginalURL)
	}
}

func TestParseDSN_DefaultPort(t *testing.T) {
	cfg, err := ParseDSN("jdbc:h2:tcp://localhost/h2-go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9092" {
		t.Errorf("Port = %q, want 9092", cfg.Port)
	}
}

func TestParseDSN_SemicolonParams(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/h2-go;USER=sa;PASSWORD=secret;IFEXISTS=TRUE"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database != "h2-go" {
		t.Errorf("Database = %q, want h2-go", cfg.Database)
	}
	if cfg.User != "sa" {
		t.Errorf("User = %q, want sa", cfg.User)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want secret", cfg.Password)
	}
	if cfg.Params["IFEXISTS"] != "TRUE" {
		t.Errorf("Params[IFEXISTS] = %q, want TRUE", cfg.Params["IFEXISTS"])
	}
	if cfg.Params["USER"] != "sa" {
		t.Errorf("Params[USER] = %q, want sa", cfg.Params["USER"])
	}
	if cfg.Params["PASSWORD"] != "secret" {
		t.Errorf("Params[PASSWORD] = %q, want secret", cfg.Params["PASSWORD"])
	}
}

func TestParseDSN_DatabaseNameRule_StripsLeadingSlash(t *testing.T) {
	// The URL path from jdbc:h2:tcp://... includes a leading "/".
	// Per PRD §7.2 the database name must be "h2-go", not "/h2-go".
	cfg, err := ParseDSN("jdbc:h2:tcp://localhost:9092/h2-go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database != "h2-go" {
		t.Errorf("Database = %q, want h2-go (no leading slash)", cfg.Database)
	}
}

func TestParseDSN_EmptyDSN(t *testing.T) {
	_, err := ParseDSN("")
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
	if !errors.Is(err, errors.New("empty DSN")) && err.Error() != "empty DSN" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseDSN_UnsupportedScheme(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"mem", "jdbc:h2:mem:test", "unsupported H2 connection mode"},
		{"file", "jdbc:h2:file:/tmp/test", "unsupported H2 connection mode"},
		{"ssl", "jdbc:h2:ssl://localhost:9092/db", "unsupported H2 connection mode"},
		{"unknown protocol", "jdbc:h2:ftp://localhost:9092/db", "unsupported H2 protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDSN(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseDSN_MissingHost(t *testing.T) {
	_, err := ParseDSN("jdbc:h2:tcp:///h2-go")
	if err == nil {
		t.Fatal("expected error for missing host, got nil")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseDSN_InvalidPort(t *testing.T) {
	_, err := ParseDSN("jdbc:h2:tcp://localhost:abc/h2-go")
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
	if !strings.Contains(err.Error(), "invalid port") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseDSN_MultipleSemicolonParams(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db;LOCK_TIMEOUT=10000;DB_CLOSE_DELAY=-1;TRACE_LEVEL_SYSTEM_OUT=2"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database != "db" {
		t.Errorf("Database = %q, want db", cfg.Database)
	}
	if cfg.Params["LOCK_TIMEOUT"] != "10000" {
		t.Errorf("Params[LOCK_TIMEOUT] = %q, want 10000", cfg.Params["LOCK_TIMEOUT"])
	}
	if cfg.Params["DB_CLOSE_DELAY"] != "-1" {
		t.Errorf("Params[DB_CLOSE_DELAY] = %q, want -1", cfg.Params["DB_CLOSE_DELAY"])
	}
	if cfg.Params["TRACE_LEVEL_SYSTEM_OUT"] != "2" {
		t.Errorf("Params[TRACE_LEVEL_SYSTEM_OUT] = %q, want 2", cfg.Params["TRACE_LEVEL_SYSTEM_OUT"])
	}
}

func TestParseDSN_ParamWithoutValue(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db;SOMEFLAG"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Params["SOMEFLAG"] != "" {
		t.Errorf("Params[SOMEFLAG] = %q, want empty string", cfg.Params["SOMEFLAG"])
	}
}

func TestParseDSN_NestedDatabasePath(t *testing.T) {
	cfg, err := ParseDSN("jdbc:h2:tcp://localhost:9092/path/to/db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database != "path/to/db" {
		t.Errorf("Database = %q, want path/to/db", cfg.Database)
	}
}

func TestParseDSN_CaseInsensitiveUserPassword(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db;user=root;password=pwd"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.User != "root" {
		t.Errorf("User = %q, want root (case-insensitive)", cfg.User)
	}
	if cfg.Password != "pwd" {
		t.Errorf("Password = %q, want pwd (case-insensitive)", cfg.Password)
	}
}
