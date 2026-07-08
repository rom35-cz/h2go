//go:build integration

package h2go

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadEnvFromFile reads a simple KEY=value .env file and returns the
// variables as a map. Lines starting with # are ignored. Empty lines
// are skipped. The file path is relative to the module root.
func loadEnvFromFile(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	vars := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		vars[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return vars
}

// integrationEnv gathers the JDBC_URL, JDBC_USER, and JDBC_PASSWORD needed
// for integration tests. It first checks the process environment, then falls
// back to h2-data/.env relative to the module root. Returns nil when not
// available.
func integrationEnv(t *testing.T) map[string]string {
	t.Helper()
	// Prefer process environment.
	url := os.Getenv("JDBC_URL")
	user := os.Getenv("JDBC_USER")
	pw := os.Getenv("JDBC_PASSWORD")
	if url != "" && user != "" {
		return map[string]string{
			"JDBC_URL":      url,
			"JDBC_USER":     user,
			"JDBC_PASSWORD": pw,
		}
	}
	// Fallback to h2-data/.env — walk up from the test binary to find it.
	for _, dir := range []string{".", "..", "../.."} {
		envPath := filepath.Join(dir, "h2-data", ".env")
		vars := loadEnvFromFile(t, envPath)
		if vars != nil && vars["JDBC_URL"] != "" && vars["JDBC_USER"] != "" {
			return vars
		}
	}
	return nil
}

func TestIntegration_Handshake(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: JDBC_URL/JDBC_USER not available in env or h2-data/.env")
	}

	url := env["JDBC_URL"]
	user := env["JDBC_USER"]
	pw := env["JDBC_PASSWORD"]

	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN %q: %v", url, err)
	}

	MergeCredentials(cfg, user, pw)

	t.Logf("connecting to %s:%s database=%s user=%s", cfg.Host, cfg.Port, cfg.Database, cfg.User)

	sess, err := Handshake(cfg)
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Logf("session close error (non-fatal): %v", err)
		}
	}()

	if sess.version != TCPProtocolVersion21 {
		t.Errorf("version = %d, want %d", sess.version, TCPProtocolVersion21)
	}
	if len(sess.id) != 64 {
		t.Errorf("session id length = %d, want 64", len(sess.id))
	}

	t.Logf("session established: version=%d autoCommit=%v id=%s...", sess.version, sess.autoCommit, sess.id[:8])
}

func TestIntegration_AuthFailure(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: JDBC_URL/JDBC_USER not available")
	}

	url := env["JDBC_URL"]

	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN %q: %v", url, err)
	}

	// Use wrong credentials — should fail with an H2Error.
	MergeCredentials(cfg, "NONEXISTENT_USER", "wrong_password")

	_, err = Handshake(cfg)
	if err == nil {
		t.Fatal("expected authentication error")
	}
	_, ok := err.(*H2Error)
	if !ok {
		t.Logf("error type = %T, value = %v", err, err)
		// Some server configurations may return a different error type (e.g.
		// connection refused if the server is not running). Only assert H2Error
		// when we actually reached the server's auth check.
		if strings.Contains(err.Error(), "unsupported H2 server") {
			t.Skip("server version mismatch — skipping auth failure test")
		}
		if strings.Contains(err.Error(), "dial") {
			t.Skip("H2 server not running — skipping auth failure test")
		}
		t.Fatalf("expected *H2Error for wrong credentials, got %T: %v", err, err)
	}
}

func TestIntegration_ParseDSNRoundTrip(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: JDBC_URL not available")
	}

	url := env["JDBC_URL"]
	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN %q: %v", url, err)
	}
	if cfg.Host == "" {
		t.Fatal("parsed host is empty")
	}
	if cfg.Port == "" {
		t.Fatal("parsed port is empty")
	}
	if cfg.Database == "" {
		t.Fatal("parsed database is empty")
	}
	if cfg.OriginalURL != url {
		t.Errorf("OriginalURL = %q, want %q", cfg.OriginalURL, url)
	}

	t.Logf("parsed: host=%s port=%s database=%s", cfg.Host, cfg.Port, cfg.Database)
}

func TestIntegration_CredentialsMerge(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	// When DSN contains no user/password, MergeCredentials fills them in.
	url := env["JDBC_URL"]
	user := env["JDBC_USER"]
	pw := env["JDBC_PASSWORD"]

	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}

	// Remember what the DSN itself provided.
	dsnUser, dsnPw := cfg.User, cfg.Password

	MergeCredentials(cfg, user, pw)

	// If DSN already had credentials, they win; otherwise merged ones are set.
	if dsnUser == "" {
		if cfg.User != user {
			t.Errorf("user after merge = %q, want %q", cfg.User, user)
		}
	} else {
		if cfg.User != dsnUser {
			t.Errorf("user after merge = %q, want %q (DSN wins)", cfg.User, dsnUser)
		}
	}
	if dsnPw == "" {
		if cfg.Password != pw {
			t.Errorf("password after merge = %q, want %q", cfg.Password, pw)
		}
	} else {
		if cfg.Password != dsnPw {
			t.Errorf("password after merge = %q, want %q (DSN wins)", cfg.Password, dsnPw)
		}
	}
}

// TestIntegration_MultipleHandshakes verifies that multiple sequential
// handshakes to the same server succeed. Each handshake is independent.
func TestIntegration_MultipleHandshakes(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	url := env["JDBC_URL"]
	user := env["JDBC_USER"]
	pw := env["JDBC_PASSWORD"]

	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, user, pw)

	for i := range 3 {
		t.Run(fmt.Sprintf("handshake_%d", i), func(t *testing.T) {
			sess, err := Handshake(cfg)
			if err != nil {
				t.Fatalf("handshake %d failed: %v", i, err)
			}
			if err := sess.Close(); err != nil {
				t.Logf("close error on handshake %d: %v", i, err)
			}
		})
	}
}
