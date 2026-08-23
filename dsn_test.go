package h2go

import (
	"strings"
	"testing"
)

// === JDBC-style DSN tests (T1.1) ===

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
	if err.Error() != "empty DSN" {
		t.Errorf("error = %q, want %q", err.Error(), "empty DSN")
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
		// ssl:// is supported since TLS transport landed; it must parse with
		// Config.TLS set (covered in TestParseDSNTLSSchemes).
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

func TestParseDSN_PortOutOfRange(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"port 0 JDBC", "jdbc:h2:tcp://localhost:0/db"},
		{"port 65536 JDBC", "jdbc:h2:tcp://localhost:65536/db"},
		{"port 0 native", "h2://localhost:0/db"},
		{"port 65536 native", "h2://localhost:65536/db"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDSN(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !strings.Contains(err.Error(), "1-65535") {
				t.Errorf("error = %q, want containing range hint '1-65535'", err.Error())
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
	input := "jdbc:h2:tcp://localhost:9092/db;IFEXISTS" // known setting, no value
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Params["IFEXISTS"] != "" {
		t.Errorf("Params[IFEXISTS] = %q, want empty string", cfg.Params["IFEXISTS"])
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

// === Native Go DSN tests (T1.2) ===

func TestParseDSN_Native_Basic(t *testing.T) {
	input := "h2://sa:secret@localhost:9092/mydb"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != "9092" {
		t.Errorf("Port = %q, want 9092", cfg.Port)
	}
	if cfg.Database != "mydb" {
		t.Errorf("Database = %q, want mydb", cfg.Database)
	}
	if cfg.User != "sa" {
		t.Errorf("User = %q, want sa", cfg.User)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want secret", cfg.Password)
	}
	if cfg.OriginalURL != input {
		t.Errorf("OriginalURL = %q, want %q", cfg.OriginalURL, input)
	}
}

func TestParseDSN_Native_ExplicitTCP(t *testing.T) {
	input := "h2+tcp://user:pass@host:9092/db"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "host" {
		t.Errorf("Host = %q, want host", cfg.Host)
	}
	if cfg.Database != "db" {
		t.Errorf("Database = %q, want db", cfg.Database)
	}
	if cfg.User != "user" {
		t.Errorf("User = %q, want user", cfg.User)
	}
	if cfg.Password != "pass" {
		t.Errorf("Password = %q, want pass", cfg.Password)
	}
}

func TestParseDSN_Native_DefaultPort(t *testing.T) {
	cfg, err := ParseDSN("h2://user:pass@localhost/db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9092" {
		t.Errorf("Port = %q, want 9092", cfg.Port)
	}
}

func TestParseDSN_Native_QueryParams(t *testing.T) {
	input := "h2://user:pass@localhost:9092/db?IFEXISTS=TRUE&LOCK_TIMEOUT=5000"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Params["IFEXISTS"] != "TRUE" {
		t.Errorf("Params[IFEXISTS] = %q, want TRUE", cfg.Params["IFEXISTS"])
	}
	if cfg.Params["LOCK_TIMEOUT"] != "5000" {
		t.Errorf("Params[LOCK_TIMEOUT] = %q, want 5000", cfg.Params["LOCK_TIMEOUT"])
	}
}

func TestParseDSN_Native_PercentDecoded(t *testing.T) {
	// Password contains a colon encoded as %3A; query value has a space.
	input := "h2://user:p%3Ass@localhost:9092/db?MODE=hello%20world"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Password != "p:ss" {
		t.Errorf("Password = %q, want p:ss", cfg.Password)
	}
	if cfg.Params["MODE"] != "hello world" {
		t.Errorf("Params[MODE] = %q, want \"hello world\"", cfg.Params["MODE"])
	}
}

func TestParseDSN_Native_NoUserinfo(t *testing.T) {
	input := "h2://localhost:9092/db?LOCK_TIMEOUT=1"
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.User != "" {
		t.Errorf("User = %q, want empty", cfg.User)
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
	if cfg.Params["LOCK_TIMEOUT"] != "1" {
		t.Errorf("Params[LOCK_TIMEOUT] = %q, want 1", cfg.Params["LOCK_TIMEOUT"])
	}
}

func TestParseDSN_Native_NestedDatabasePath(t *testing.T) {
	cfg, err := ParseDSN("h2://user:pass@localhost:9092/path/to/db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database != "path/to/db" {
		t.Errorf("Database = %q, want path/to/db", cfg.Database)
	}
}

func TestParseDSN_Native_MissingHost(t *testing.T) {
	_, err := ParseDSN("h2:///db")
	if err == nil {
		t.Fatal("expected error for missing host, got nil")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseDSN_Native_InvalidPort(t *testing.T) {
	_, err := ParseDSN("h2://user:pass@localhost:abc/db")
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
	if !strings.Contains(err.Error(), "invalid port") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseDSN_Native_UnsupportedScheme(t *testing.T) {
	cases := []string{
		"http://localhost:9092/db",
		"postgres://localhost:9092/db",
		"jdbc:h2:mem:test",
		// h2+ssl:// is supported since TLS transport landed; it must parse
		// with Config.TLS set (covered in TestParseDSNTLSSchemes).
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDSN(input)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", input)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("error = %q, want containing 'unsupported'", err.Error())
			}
		})
	}
}

// === Credential merge tests (T1.2) ===

func TestMergeCredentials_FillsEmpty(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "db",
	}
	MergeCredentials(cfg, "envuser", "envpass")
	if cfg.User != "envuser" {
		t.Errorf("User = %q, want envuser", cfg.User)
	}
	if cfg.Password != "envpass" {
		t.Errorf("Password = %q, want envpass", cfg.Password)
	}
}

func TestMergeCredentials_PreservesExisting(t *testing.T) {
	cfg := &Config{
		User:     "dsnuser",
		Password: "dsnpass",
	}
	MergeCredentials(cfg, "envuser", "envpass")
	if cfg.User != "dsnuser" {
		t.Errorf("User = %q, want dsnuser (DSN takes precedence)", cfg.User)
	}
	if cfg.Password != "dsnpass" {
		t.Errorf("Password = %q, want dsnpass (DSN takes precedence)", cfg.Password)
	}
}

func TestMergeCredentials_PartialOverlay(t *testing.T) {
	cfg := &Config{
		User: "dsnuser",
	}
	MergeCredentials(cfg, "envuser", "envpass")
	if cfg.User != "dsnuser" {
		t.Errorf("User = %q, want dsnuser", cfg.User)
	}
	if cfg.Password != "envpass" {
		t.Errorf("Password = %q, want envpass", cfg.Password)
	}
}

func TestMergeCredentials_EmptyEnvironment(t *testing.T) {
	cfg := &Config{
		User:     "dsnuser",
		Password: "dsnpass",
	}
	MergeCredentials(cfg, "", "")
	if cfg.User != "dsnuser" {
		t.Errorf("User = %q, want dsnuser", cfg.User)
	}
	if cfg.Password != "dsnpass" {
		t.Errorf("Password = %q, want dsnpass", cfg.Password)
	}
}

// === DSN parameter policy tests (Tier A production hardening) ===

func TestParseDSN_UnknownSettingRejected(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db;MY_FLAG=1"
	_, err := ParseDSN(input)
	if err == nil {
		t.Fatal("expected rejection of unknown setting")
	}
	for _, want := range []string{"MY_FLAG", "IGNORE_UNKNOWN_SETTINGS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}

	// Escape hatch mirrors H2 JDBC semantics.
	cfg, err := ParseDSN(input + ";IGNORE_UNKNOWN_SETTINGS=TRUE")
	if err != nil {
		t.Fatalf("IGNORE_UNKNOWN_SETTINGS=TRUE must tolerate unknown settings: %v", err)
	}
	if cfg.Params["MY_FLAG"] != "1" {
		t.Errorf("unknown param not preserved: %v", cfg.Params)
	}
}

func TestParseDSN_KnownSettingsAccepted(t *testing.T) {
	input := "jdbc:h2:tcp://localhost:9092/db" +
		";IFEXISTS=TRUE;ACCESS_MODE_DATA=r;MODE=Legacy;LOCK_TIMEOUT=2500" +
		";AUTO_SERVER=TRUE;TRACE_LEVEL_FILE=0;NON_KEYWORDS=VALUE" +
		";ifexists=TRUE" // case-insensitive duplicate, same value: kept once
	cfg, err := ParseDSN(input)
	if err != nil {
		t.Fatalf("known settings rejected: %v", err)
	}
	if cfg.Params["IFEXISTS"] != "TRUE" {
		t.Errorf("IFEXISTS = %q", cfg.Params["IFEXISTS"])
	}
	if len(cfg.Params) != 7 {
		t.Errorf("Params has %d entries, want 7 (duplicate spelling collapsed): %v", len(cfg.Params), cfg.Params)
	}

	// Conflicting duplicate is a parse error, like H2's DUPLICATE_PROPERTY.
	if _, err := ParseDSN("jdbc:h2:tcp://localhost:9092/db;IFEXISTS=TRUE;ifexists=false"); err == nil {
		t.Error("conflicting duplicate setting: expected error")
	}
	if cfg.Params["ACCESS_MODE_DATA"] != "r" {
		t.Errorf("ACCESS_MODE_DATA = %q", cfg.Params["ACCESS_MODE_DATA"])
	}
}

func TestSessionPropertyMap(t *testing.T) {
	params := map[string]string{
		"ifexists":         "TRUE",
		"ACCESS_MODE_DATA": "r",
		"MODE":             "MySQL",
		"AUTO_SERVER":      "TRUE", // local-only: must not be forwarded
		"TRACE_LEVEL_FILE": "0",    // local-only
		"MyFlag":           "x",    // tolerated unknown: must not be forwarded
	}
	got := sessionPropertyMap(params)
	want := [][2]string{
		{"ACCESS_MODE_DATA", "r"},
		{"IFEXISTS", "TRUE"},
		{"MODE", "MySQL"},
	}
	if len(got) != len(want) {
		t.Fatalf("property map = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("property[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	if empty := sessionPropertyMap(nil); len(empty) != 0 {
		t.Errorf("nil params should produce empty map, got %v", empty)
	}
}

func TestValidateParams_IgnoreFlagParsing(t *testing.T) {
	cases := []struct {
		value   string
		rejects bool
	}{
		{"TRUE", false},
		{"true", false},
		{"1", false},
		{"FALSE", true},
		{"", true},
		{"yes", false},
	}
	for _, tc := range cases {
		params := map[string]string{
			"SOMETHING":               "1",
			"IGNORE_UNKNOWN_SETTINGS": tc.value,
		}
		err := validateParams(params)
		if tc.rejects && err == nil {
			t.Errorf("IGNORE_UNKNOWN_SETTINGS=%q: expected rejection", tc.value)
		}
		if !tc.rejects && err != nil {
			t.Errorf("IGNORE_UNKNOWN_SETTINGS=%q: unexpected error %v", tc.value, err)
		}
	}
}
