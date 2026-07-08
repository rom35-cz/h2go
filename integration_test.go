//go:build integration

package h2go

import (
	"context"
	"database/sql"
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

// TestIntegration_DriverOpenDB exercises the database/sql driver
// integration: open a *sql.DB, get a connection, close it cleanly.
// This verifies the T4.1 Driver, Connector, and Conn wiring.
func TestIntegration_DriverOpenDB(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	url := env["JDBC_URL"]
	user := env["JDBC_USER"]
	pw := env["JDBC_PASSWORD"]

	// Parse the DSN from env.
	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	// Overlay credentials from environment.
	MergeCredentials(cfg, user, pw)

	t.Logf("opening db: host=%s port=%s database=%s user=%s", cfg.Host, cfg.Port, cfg.Database, cfg.User)

	// Open database via config-based API (T4.1 requirement).
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("db.Close error (may be expected): %v", err)
		}
	}()

	// Verify we can get a raw connection (validates Connector.Connect and conn creation).
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("db.Conn failed: %v", err)
	}

	// Exercise the raw driver's Close method.
	err = conn.Close()
	if err != nil {
		t.Errorf("conn.Close error: %v", err)
	}

	t.Log("driver integration test passed: OpenDB -> Conn -> Close works")
}

// TestIntegration_DriverOpenDSN exercises sql.Open("h2", dsn) directly,
// then verifies the connection can be closed.
func TestIntegration_DriverOpenDSN(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	url := env["JDBC_URL"]
	user := env["JDBC_USER"]
	pw := env["JDBC_PASSWORD"]

	// Merge credentials into a native DSN for cleaner url handling.
	cfg, err := ParseDSN(url)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, user, pw)

	// Build a native DSN that includes credentials.
	dsn := fmt.Sprintf("h2://%s:%s@%s:%s/%s", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	t.Logf("opening db with DSN: h2://%s:***@%s:%s/%s", cfg.User, cfg.Host, cfg.Port, cfg.Database)

	// Open via sql.Open with DSN (verifies Driver.Open and Driver.OpenConnector).
	db, err := sql.Open("h2", dsn)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("db.Close error: %v", err)
		}
	}()

	// Get a raw connection.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("db.Conn failed: %v", err)
	}

	// Close the raw connection.
	if err := conn.Close(); err != nil {
		t.Errorf("conn.Close error: %v", err)
	}

	t.Log("driver DSN integration test passed: sql.Open -> Conn -> Close works")
}

// TestIntegration_Ping verifies db.PingContext succeeds against a live H2 server.
// This tests the driver.Pinger implementation with a real round-trip.
func TestIntegration_Ping(t *testing.T) {
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

	t.Logf("pinging: host=%s port=%s database=%s", cfg.Host, cfg.Port, cfg.Database)

	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("db.Close error: %v", err)
		}
	}()

	// Ping should succeed against a live server.
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext failed: %v", err)
	}

	t.Log("ping successful: connection is alive")
}

// TestIntegration_QueryContext exercises db.QueryContext with a real SELECT query.
// It verifies: column names, row count, typed values, and clean close.
func TestIntegration_QueryContext(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)

	ctx := context.Background()

	// SELECT 1 — simplest possible query.
	rows, err := db.QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("QueryContext(SELECT 1) failed: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() failed: %v", err)
	}
	if len(cols) != 1 {
		t.Fatalf("expected 1 column, got %d", len(cols))
	}

	if !rows.Next() {
		t.Fatal("expected one row")
	}
	var v int64
	if err := rows.Scan(&v); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if v != 1 {
		t.Errorf("SELECT 1: got %d, want 1", v)
	}
	if rows.Next() {
		t.Error("expected only one row")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	t.Log("SELECT 1 passed")
}

// TestIntegration_QuerySelect exercises a multi-row, multi-column SELECT over seed data.
func TestIntegration_QuerySelect(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)

	ctx := context.Background()

	rows, err := db.QueryContext(ctx, "SELECT id, name FROM people ORDER BY id")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() failed: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d: %v", len(cols), cols)
	}

	var count int
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("Scan row %d: %v", count, err)
		}
		t.Logf("  row %d: id=%d name=%s", count, id, name)
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one row from people table")
	}
	t.Logf("SELECT people: %d rows returned", count)
}

// TestIntegration_ExecContext exercises db.ExecContext with DDL and DML statements.
func TestIntegration_ExecContext(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)

	ctx := context.Background()

	// CREATE TABLE (DDL).
	_, err := db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS t53_exec (id INT PRIMARY KEY, val VARCHAR(100))")
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	t.Log("CREATE TABLE OK")

	// Clean up any leftover rows from previous run.
	_, _ = db.ExecContext(ctx, "DELETE FROM t53_exec")

	// INSERT.
	res, err := db.ExecContext(ctx, "INSERT INTO t53_exec VALUES (1, 'hello')")
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected: %v", err)
	}
	if affected != 1 {
		t.Errorf("INSERT: expected 1 affected row, got %d", affected)
	}
	t.Logf("INSERT affected %d row(s)", affected)

	// UPDATE.
	res, err = db.ExecContext(ctx, "UPDATE t53_exec SET val = 'world' WHERE id = 1")
	if err != nil {
		t.Fatalf("UPDATE failed: %v", err)
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		t.Errorf("UPDATE: expected 1 affected row, got %d", affected)
	}
	t.Logf("UPDATE affected %d row(s)", affected)

	// DELETE.
	res, err = db.ExecContext(ctx, "DELETE FROM t53_exec WHERE id = 1")
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		t.Errorf("DELETE: expected 1 affected row, got %d", affected)
	}
	t.Logf("DELETE affected %d row(s)", affected)

	// DROP TABLE.
	_, err = db.ExecContext(ctx, "DROP TABLE IF EXISTS t53_exec")
	if err != nil {
		t.Fatalf("DROP TABLE failed: %v", err)
	}
	t.Log("DROP TABLE OK")
}

// TestIntegration_QueryLargeResult checks that a result larger than one fetch
// batch is correctly paginated.
func TestIntegration_QueryLargeResult(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)

	ctx := context.Background()

	// system_range(1, N) generates N rows without needing a real table.
	const n = 250 // larger than default fetch size (100)
	rows, err := db.QueryContext(ctx,
		"SELECT x FROM SYSTEM_RANGE(1, 250)")
	if err != nil {
		t.Fatalf("QueryContext(system_range) failed: %v", err)
	}
	defer rows.Close()

	var prev, count int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if x != prev+1 {
			t.Fatalf("gap: got %d after %d", x, prev)
		}
		prev = x
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if count != n {
		t.Errorf("expected %d rows, got %d", n, count)
	}
	t.Logf("large result: %d rows fetched in batches", count)
}

// integrationDB opens a *sql.DB from env credentials and registers a cleanup.
func integrationDB(t *testing.T, env map[string]string) *sql.DB {
	t.Helper()
	cfg, err := ParseDSN(env["JDBC_URL"])
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
