//go:build integration

package h2go

import (
	"bytes"
	"context"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

// TestIntegration_ExecContextLastInsertId verifies generated keys are exposed
// through Result.LastInsertId() for single numeric keys, and remain
// unavailable for non-numeric keys.
func TestIntegration_ExecContextLastInsertId(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t65_lastid_num (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		note VARCHAR(100)
	)`)
	if err != nil {
		t.Fatalf("CREATE numeric-key table failed: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS t65_lastid_num") }()
	_, _ = db.ExecContext(ctx, "DELETE FROM t65_lastid_num")

	res, err := db.ExecContext(ctx, "INSERT INTO t65_lastid_num (note) VALUES (?)", "first")
	if err != nil {
		t.Fatalf("numeric-key INSERT failed: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId numeric-key: %v", err)
	}
	if id <= 0 {
		t.Fatalf("numeric-key LastInsertId=%d, want > 0", id)
	}
	var note string
	if err := db.QueryRowContext(ctx, "SELECT note FROM t65_lastid_num WHERE id = ?", id).Scan(&note); err != nil {
		t.Fatalf("verify numeric generated key failed: %v", err)
	}
	if note != "first" {
		t.Fatalf("verify numeric generated key note=%q, want first", note)
	}

	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t65_lastid_text (
		id VARCHAR(40) PRIMARY KEY,
		note VARCHAR(100)
	)`)
	if err != nil {
		t.Fatalf("CREATE non-numeric-key table failed: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS t65_lastid_text") }()
	_, _ = db.ExecContext(ctx, "DELETE FROM t65_lastid_text")

	res, err = db.ExecContext(ctx, "INSERT INTO t65_lastid_text (id, note) VALUES (?, ?)", "alpha", "second")
	if err != nil {
		t.Fatalf("non-numeric-key INSERT failed: %v", err)
	}
	id, err = res.LastInsertId()
	if err == nil {
		t.Fatal("expected LastInsertId error for non-numeric generated key")
	}
	if !errors.Is(err, ErrLastInsertIDUnavailable) {
		t.Fatalf("expected ErrLastInsertIDUnavailable, got %v", err)
	}
	if id != 0 {
		t.Fatalf("non-numeric-key LastInsertId=%d, want 0", id)
	}
}

// TestIntegration_GeneratedKeysMultiColumn verifies that the GeneratedKeys
// result on the driver's Result exposes multi-column generated keys when
// column numbers are specified, and that LastInsertId() returns
// ErrLastInsertIDUnavailable for multi-column results.
func TestIntegration_GeneratedKeysMultiColumn(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	// Use a custom connector with explicit column numbers for generated keys.
	cfg, err := ParseDSN(env["JDBC_URL"])
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
	cfg.GeneratedKeysMode = GeneratedKeysColumnNumbers
	cfg.GeneratedKeysColumns = []int{1, 2} // request both identity columns
	cfg.GeneratedKeysModeSet = true

	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	table := integrationTxTableName("genkeys_multi")
	_, err = db.ExecContext(ctx, `CREATE TABLE `+table+` (
		id1 BIGINT GENERATED BY DEFAULT AS IDENTITY,
		id2 BIGINT GENERATED BY DEFAULT AS IDENTITY,
		note VARCHAR(100)
	)`)
	if err != nil {
		t.Fatalf("CREATE multi-column table failed: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table) })

	// Use conn.Raw to access the underlying driver.Conn and cast the result.
	rawConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer rawConn.Close()

	var h2Res *result
	err = rawConn.Raw(func(raw any) error {
		c := raw.(*conn)
		driverRes, err := c.ExecContext(ctx, "INSERT INTO "+table+" (note) VALUES (?)", []driver.NamedValue{
			{Ordinal: 1, Value: "multi"},
		})
		if err != nil {
			return err
		}
		var ok bool
		h2Res, ok = driverRes.(*result)
		if !ok {
			t.Fatalf("expected *result, got %T", driverRes)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ExecContext via Raw: %v", err)
	}

	// LastInsertId should fail for multi-column keys.
	_, err = h2Res.LastInsertId()
	if err == nil {
		t.Fatal("expected LastInsertId error for multi-column generated keys")
	}
	if !errors.Is(err, ErrLastInsertIDUnavailable) {
		t.Fatalf("expected ErrLastInsertIDUnavailable, got %v", err)
	}

	if h2Res.GeneratedKeys == nil {
		t.Fatal("GeneratedKeys is nil")
	}
	if len(h2Res.GeneratedKeys.Rows) == 0 {
		t.Fatal("no generated key rows")
	}
	t.Logf("GeneratedKeys: columns=%v rows=%d", h2Res.GeneratedKeys.Columns, len(h2Res.GeneratedKeys.Rows))
	if len(h2Res.GeneratedKeys.Columns) != 2 {
		t.Errorf("expected 2 generated key columns, got %d", len(h2Res.GeneratedKeys.Columns))
	}
}

// TestIntegration_GeneratedKeysNoKeys verifies that when GeneratedKeysMode is
// set to GeneratedKeysNone, no generated keys are requested and LastInsertId
// returns ErrLastInsertIDUnavailable.
func TestIntegration_GeneratedKeysNoKeys(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	cfg, err := ParseDSN(env["JDBC_URL"])
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
	cfg.GeneratedKeysMode = GeneratedKeysNone
	cfg.GeneratedKeysModeSet = true

	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	table := integrationTxTableName("genkeys_none")
	_, err = db.ExecContext(ctx, `CREATE TABLE `+table+` (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		note VARCHAR(100)
	)`)
	if err != nil {
		t.Fatalf("CREATE table failed: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table) })

	// Use Raw to access the underlying result and check the mode.
	rawConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer rawConn.Close()

	err = rawConn.Raw(func(raw any) error {
		c := raw.(*conn)
		// Verify the session config has GeneratedKeysNone.
		if c.sess.cfg.GeneratedKeysMode != GeneratedKeysNone {
			t.Errorf("session cfg.GeneratedKeysMode = %d, want %d", c.sess.cfg.GeneratedKeysMode, GeneratedKeysNone)
		}
		// Verify generatedKeysMode returns None.
		if mode := c.sess.generatedKeysMode(); mode != GeneratedKeysNone {
			t.Errorf("generatedKeysMode() = %d, want %d", mode, GeneratedKeysNone)
		}
		// Exec without args through the driver's ExecContext.
		driverRes, err := c.ExecContext(ctx, "INSERT INTO "+table+" (note) VALUES (?)", []driver.NamedValue{
			{Ordinal: 1, Value: "no-key"},
		})
		if err != nil {
			return err
		}
		_, err = driverRes.LastInsertId()
		if err == nil {
			t.Fatal("expected LastInsertId error when GeneratedKeysMode=none")
		}
		if !errors.Is(err, ErrLastInsertIDUnavailable) {
			t.Fatalf("expected ErrLastInsertIDUnavailable, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
}

// TestIntegration_ExecContextWithParams validates positional parameter binding
// for ExecContext using T6.1 value encoding (including metadata-aware NUMERIC
// and UUID string handling).
func TestIntegration_ExecContextWithParams(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t62_exec_params (
		id INT PRIMARY KEY,
		amount NUMERIC(12,4),
		uid UUID,
		ts_tz TIMESTAMP WITH TIME ZONE,
		payload VARBINARY
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS t62_exec_params")
	}()

	_, _ = db.ExecContext(ctx, "DELETE FROM t62_exec_params")

	ts := time.Date(2026, 7, 9, 14, 15, 16, 987654000, time.FixedZone("+05", 5*3600))
	uuidVal := uuid.MustParse("12345678-1234-5678-9abc-def012345678")

	res, err := db.ExecContext(ctx,
		"INSERT INTO t62_exec_params (id, amount, uid, ts_tz, payload) VALUES (?, ?, ?, ?, ?)",
		7, "12345.6789", uuidVal, ts, []byte{0xDE, 0xAD, 0xBE, 0xEF})
	if err != nil {
		t.Fatalf("INSERT with params failed: %v", err)
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		t.Fatalf("INSERT affected %d rows, want 1", affected)
	}

	res, err = db.ExecContext(ctx,
		"UPDATE t62_exec_params SET amount = ? WHERE id = ?",
		"555.0001", 7)
	if err != nil {
		t.Fatalf("UPDATE with params failed: %v", err)
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		t.Fatalf("UPDATE affected %d rows, want 1", affected)
	}

	var (
		amount  string
		uid     string
		tsRead  time.Time
		payload []byte
	)
	if err := db.QueryRowContext(ctx,
		"SELECT amount, uid, ts_tz, payload FROM t62_exec_params WHERE id = 7").
		Scan(&amount, &uid, &tsRead, &payload); err != nil {
		t.Fatalf("SELECT verification failed: %v", err)
	}
	if amount != "555.0001" {
		t.Fatalf("amount: got %q, want 555.0001", amount)
	}
	if strings.ToLower(uid) != uuidVal.String() {
		t.Fatalf("uid: got %q, want %q", uid, uuidVal.String())
	}
	if !bytes.Equal(payload, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Fatalf("payload: got %X", payload)
	}
	_, offset := tsRead.Zone()
	if offset != 5*3600 {
		t.Fatalf("ts_tz offset: got %d, want %d", offset, 5*3600)
	}
	if !tsRead.UTC().Equal(ts.UTC()) {
		t.Fatalf("ts_tz UTC: got %v, want %v", tsRead.UTC(), ts.UTC())
	}

	res, err = db.ExecContext(ctx, "DELETE FROM t62_exec_params WHERE id = ?", 7)
	if err != nil {
		t.Fatalf("DELETE with params failed: %v", err)
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		t.Fatalf("DELETE affected %d rows, want 1", affected)
	}
}

// TestIntegration_QueryContextWithParams validates the inline parameterised
// QueryContext path added in the Phase 6 review (Bug 1 fix).  This path is
// distinct from the prepared-statement path used by TestIntegration_PreparedStatements.
func TestIntegration_QueryContextWithParams(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t64_qcp (
		id INT PRIMARY KEY, name VARCHAR(50), score DOUBLE
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS t64_qcp") }()
	_, _ = db.ExecContext(ctx, "DELETE FROM t64_qcp")

	for _, row := range []struct {
		id    int
		name  string
		score float64
	}{
		{1, "alice", 9.5},
		{2, "bob", 7.0},
		{3, "carol", 8.25},
	} {
		_, err = db.ExecContext(ctx, "INSERT INTO t64_qcp VALUES (?, ?, ?)",
			row.id, row.name, row.score)
		if err != nil {
			t.Fatalf("INSERT id=%d failed: %v", row.id, err)
		}
	}

	// Parameterised QueryContext — goes through the inline path (not ErrSkip+Prepare).
	rows, err := db.QueryContext(ctx,
		"SELECT name, score FROM t64_qcp WHERE id > ? ORDER BY id", 1)
	if err != nil {
		t.Fatalf("QueryContext with param failed: %v", err)
	}
	defer rows.Close()

	type got struct {
		name  string
		score float64
	}
	var results []got
	for rows.Next() {
		var g got
		if err := rows.Scan(&g.name, &g.score); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		results = append(results, g)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	want := []got{{"bob", 7.0}, {"carol", 8.25}}
	if len(results) != len(want) {
		t.Fatalf("got %d rows, want %d", len(results), len(want))
	}
	for i, w := range want {
		if results[i].name != w.name || results[i].score != w.score {
			t.Fatalf("row %d: got %+v, want %+v", i, results[i], w)
		}
	}
}

// TestIntegration_PreparedStatements exercises prepare/exec/query/close paths.
func TestIntegration_PreparedStatements(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t63_stmt (
		id INT PRIMARY KEY,
		name VARCHAR(100),
		amount NUMERIC(12,4)
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS t63_stmt")
	}()
	_, _ = db.ExecContext(ctx, "DELETE FROM t63_stmt")

	ins, err := db.PrepareContext(ctx, "INSERT INTO t63_stmt (id, name, amount) VALUES (?, ?, ?)")
	if err != nil {
		t.Fatalf("Prepare INSERT failed: %v", err)
	}
	defer ins.Close()

	for _, tc := range []struct {
		id     int
		name   string
		amount string
	}{
		{1, "alice", "10.5000"},
		{2, "bob", "20.2500"},
		{3, "carol", "30.0001"},
	} {
		res, err := ins.ExecContext(ctx, tc.id, tc.name, tc.amount)
		if err != nil {
			t.Fatalf("INSERT id=%d failed: %v", tc.id, err)
		}
		affected, _ := res.RowsAffected()
		if affected != 1 {
			t.Fatalf("INSERT id=%d affected %d rows, want 1", tc.id, affected)
		}
	}

	upd, err := db.PrepareContext(ctx, "UPDATE t63_stmt SET name = ? WHERE id = ?")
	if err != nil {
		t.Fatalf("Prepare UPDATE failed: %v", err)
	}
	defer upd.Close()

	res, err := upd.ExecContext(ctx, "bobby", 2)
	if err != nil {
		t.Fatalf("UPDATE failed: %v", err)
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		t.Fatalf("UPDATE affected %d rows, want 1", affected)
	}

	sel, err := db.PrepareContext(ctx, "SELECT name, amount FROM t63_stmt WHERE id = ?")
	if err != nil {
		t.Fatalf("Prepare SELECT failed: %v", err)
	}

	for _, tc := range []struct {
		id         int
		wantName   string
		wantAmount string
	}{
		{1, "alice", "10.5000"},
		{2, "bobby", "20.2500"},
		{3, "carol", "30.0001"},
	} {
		var gotName, gotAmount string
		if err := sel.QueryRowContext(ctx, tc.id).Scan(&gotName, &gotAmount); err != nil {
			t.Fatalf("SELECT id=%d failed: %v", tc.id, err)
		}
		if gotName != tc.wantName || gotAmount != tc.wantAmount {
			t.Fatalf("SELECT id=%d got (%q, %q), want (%q, %q)",
				tc.id, gotName, gotAmount, tc.wantName, tc.wantAmount)
		}
	}

	if err := sel.Close(); err != nil {
		t.Fatalf("Stmt.Close failed: %v", err)
	}
	if err := sel.Close(); err != nil {
		t.Fatalf("Stmt.Close second call failed: %v", err)
	}
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

// TestIntegration_MaxRows verifies that Config.MaxRows is forwarded to the
// server as the protocol maxRows, bounding the server-side result set, and
// that Config.FetchSize controls the prefetch batch size.
func TestIntegration_MaxRows(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	newDB := func(t *testing.T, maxRows int64, fetchSize int) *sql.DB {
		t.Helper()
		cfg, err := ParseDSN(env["JDBC_URL"])
		if err != nil {
			t.Fatalf("ParseDSN: %v", err)
		}
		MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
		cfg.MaxRows = maxRows
		cfg.FetchSize = fetchSize
		db, err := OpenDB(cfg)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}

	count := func(t *testing.T, db *sql.DB, query string, args ...any) int {
		t.Helper()
		rows, err := db.QueryContext(context.Background(), query, args...)
		if err != nil {
			t.Fatalf("QueryContext %q: %v", query, err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var x int64
			if err := rows.Scan(&x); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Err: %v", err)
		}
		return n
	}

	// MaxRows=3 bounds the server-side result for an inline query.
	db := newDB(t, 3, 0)
	if got := count(t, db, "SELECT x FROM SYSTEM_RANGE(1, 10)"); got != 3 {
		t.Errorf("inline query with MaxRows=3: got %d rows, want 3", got)
	}

	// MaxRows=3 also bounds prepared statement queries.
	if got := count(t, db, "SELECT x FROM SYSTEM_RANGE(1, ?)", 10); got != 3 {
		t.Errorf("prepared query with MaxRows=3: got %d rows, want 3", got)
	}

	// MaxRows=0 keeps the default unlimited behavior.
	db = newDB(t, 0, 0)
	if got := count(t, db, "SELECT x FROM SYSTEM_RANGE(1, 10)"); got != 10 {
		t.Errorf("unlimited query: got %d rows, want 10", got)
	}

	// FetchSize larger than the row count still returns all rows (single batch).
	db = newDB(t, 0, 500)
	if got := count(t, db, "SELECT x FROM SYSTEM_RANGE(1, 10)"); got != 10 {
		t.Errorf("FetchSize=500: got %d rows, want 10", got)
	}

	// FetchSize=2 forces multiple fetch batches across a larger range.
	db = newDB(t, 0, 2)
	if got := count(t, db, "SELECT x FROM SYSTEM_RANGE(1, 7)"); got != 7 {
		t.Errorf("FetchSize=2: got %d rows, want 7", got)
	}
}

// TestIntegration_ScalarTypeDecoding exercises the MVP scalar decode paths
// end-to-end against a live server. This directly validates the Phase 5
// review fixes for DATE (Bug A), TIMESTAMP WITH TIME ZONE (Bug B), and the
// remaining scalar decoders (BOOLEAN, integer widths, REAL/DOUBLE, NUMERIC,
// UUID, TIME, VARCHAR, VARBINARY), which have unit coverage but were not
// asserted against a real H2 wire encoding.
func TestIntegration_ScalarTypeDecoding(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	row := db.QueryRowContext(ctx, `SELECT
		CAST(TRUE AS BOOLEAN),
		CAST(42 AS TINYINT),
		CAST(1234 AS SMALLINT),
		CAST(70000 AS INTEGER),
		CAST(9000000000 AS BIGINT),
		CAST(3.5 AS REAL),
		CAST(2.718281828 AS DOUBLE PRECISION),
		CAST(12345.6789 AS NUMERIC(9,4)),
		CAST('hello world' AS VARCHAR),
		CAST(X'DEADBEEF' AS VARBINARY),
		CAST('2021-03-14' AS DATE),
		CAST('13:37:00' AS TIME),
		CAST('2021-03-14 15:09:26.535' AS TIMESTAMP),
		CAST('2021-03-14 15:09:26.535+05:00' AS TIMESTAMP WITH TIME ZONE),
		CAST('12345678-1234-5678-9abc-def012345678' AS UUID)`)

	var (
		vBool   bool
		vTiny   int64
		vSmall  int64
		vInt    int64
		vBig    int64
		vReal   float64
		vDouble float64
		vNum    string
		vStr    string
		vBin    []byte
		vDate   time.Time
		vTime   time.Time
		vTs     time.Time
		vTsTZ   time.Time
		vUUID   string
	)
	if err := row.Scan(&vBool, &vTiny, &vSmall, &vInt, &vBig, &vReal, &vDouble,
		&vNum, &vStr, &vBin, &vDate, &vTime, &vTs, &vTsTZ, &vUUID); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if !vBool {
		t.Error("BOOLEAN: got false, want true")
	}
	if vTiny != 42 || vSmall != 1234 || vInt != 70000 || vBig != 9000000000 {
		t.Errorf("integers: got %d/%d/%d/%d", vTiny, vSmall, vInt, vBig)
	}
	if vReal < 3.49 || vReal > 3.51 {
		t.Errorf("REAL: got %v, want ~3.5", vReal)
	}
	if vDouble < 2.7182 || vDouble > 2.7183 {
		t.Errorf("DOUBLE: got %v, want ~2.718281828", vDouble)
	}
	if vNum != "12345.6789" {
		t.Errorf("NUMERIC: got %q, want 12345.6789", vNum)
	}
	if vStr != "hello world" {
		t.Errorf("VARCHAR: got %q", vStr)
	}
	if fmt.Sprintf("%X", vBin) != "DEADBEEF" {
		t.Errorf("VARBINARY: got %X, want DEADBEEF", vBin)
	}
	// Bug A regression: DATE must unpack the packed dateValue, not treat it
	// as days-since-epoch.
	if vDate.Year() != 2021 || vDate.Month() != time.March || vDate.Day() != 14 {
		t.Errorf("DATE: got %04d-%02d-%02d, want 2021-03-14",
			vDate.Year(), vDate.Month(), vDate.Day())
	}
	if vTime.Hour() != 13 || vTime.Minute() != 37 || vTime.Second() != 0 {
		t.Errorf("TIME: got %02d:%02d:%02d, want 13:37:00",
			vTime.Hour(), vTime.Minute(), vTime.Second())
	}
	if vTs.Year() != 2021 || vTs.Hour() != 15 || vTs.Minute() != 9 ||
		vTs.Nanosecond() != 535000000 {
		t.Errorf("TIMESTAMP: got %v, want 2021-03-14 15:09:26.535", vTs)
	}
	// Bug B regression: TIMESTAMP WITH TIME ZONE carries local wall-clock time
	// plus an offset; the resulting instant must be correct in UTC.
	_, offset := vTsTZ.Zone()
	if offset != 5*3600 {
		t.Errorf("TIMESTAMP_TZ: zone offset got %ds, want 18000s", offset)
	}
	wantUTC := time.Date(2021, 3, 14, 10, 9, 26, 535000000, time.UTC)
	if !vTsTZ.UTC().Equal(wantUTC) {
		t.Errorf("TIMESTAMP_TZ: UTC instant got %v, want %v", vTsTZ.UTC(), wantUTC)
	}
	if vUUID != "12345678-1234-5678-9abc-def012345678" {
		t.Errorf("UUID: got %q", vUUID)
	}
	t.Log("scalar type decoding passed")
}

// TestIntegration_ScalarRoundTripTable exercises representative scalar round
// trips through the server with a table-driven matrix. It focuses on small,
// easily reproducible cases so failures point to a specific H2 type family.
func TestIntegration_ScalarRoundTripTable(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	cases := []struct {
		name  string
		query string
		args  []any
		scan  func(*testing.T, *sql.Row)
	}{
		{
			name:  "boolean",
			query: "SELECT CAST(? AS BOOLEAN)",
			args:  []any{true},
			scan: func(t *testing.T, row *sql.Row) {
				t.Helper()
				var got bool
				if err := row.Scan(&got); err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if !got {
					t.Fatal("expected true")
				}
			},
		},
		{
			name:  "bigint",
			query: "SELECT CAST(? AS BIGINT)",
			args:  []any{int64(9000000000)},
			scan: func(t *testing.T, row *sql.Row) {
				t.Helper()
				var got int64
				if err := row.Scan(&got); err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if got != 9000000000 {
					t.Fatalf("got %d, want 9000000000", got)
				}
			},
		},
		{
			name:  "numeric",
			query: "SELECT CAST(? AS NUMERIC(9,4))",
			args:  []any{"12345.6789"},
			scan: func(t *testing.T, row *sql.Row) {
				t.Helper()
				var got string
				if err := row.Scan(&got); err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if got != "12345.6789" {
					t.Fatalf("got %q, want 12345.6789", got)
				}
			},
		},
		{
			name:  "varchar",
			query: "SELECT CAST(? AS VARCHAR)",
			args:  []any{"hello world"},
			scan: func(t *testing.T, row *sql.Row) {
				t.Helper()
				var got string
				if err := row.Scan(&got); err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if got != "hello world" {
					t.Fatalf("got %q, want hello world", got)
				}
			},
		},
		{
			name:  "varbinary",
			query: "SELECT CAST(? AS VARBINARY)",
			args:  []any{[]byte{0xDE, 0xAD, 0xBE, 0xEF}},
			scan: func(t *testing.T, row *sql.Row) {
				t.Helper()
				var got []byte
				if err := row.Scan(&got); err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if fmt.Sprintf("%X", got) != "DEADBEEF" {
					t.Fatalf("got %X, want DEADBEEF", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := db.QueryRowContext(ctx, tc.query, tc.args...)
			tc.scan(t, row)
		})
	}
}

// TestIntegration_TypeShowcaseFullSelect selects every column of the seeded
// type_showcase table and asserts the documented Go representation for each
// MVP-supported type. It is the single widest regression test for the
// supported-type matrix.
//
// Columns covered (per seed.sql): TINYINT, SMALLINT, INTEGER, BIGINT, REAL,
// DOUBLE, DECIMAL, DECFLOAT, BOOLEAN, VARCHAR, CHAR, VARCHAR_IGNORECASE,
// BINARY, VARBINARY, CLOB, BLOB, DATE, TIME, TIME WITH TIME ZONE, TIMESTAMP,
// TIMESTAMP WITH TIME ZONE, UUID. JSON is intentionally not in the SELECT *
// (it is decoded as ErrUnsupportedType today) and is probed separately below.
func TestIntegration_TypeShowcaseFullSelect(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	const query = `SELECT
		id, col_tinyint, col_smallint, col_integer, col_bigint,
		col_real, col_double, col_decimal, col_decfloat,
		col_boolean_t, col_boolean_f,
		col_varchar, col_char, col_varchar_ic,
		col_binary, col_varbinary,
		col_clob, col_blob,
		col_date, col_time, col_time_tz, col_timestamp, col_timestamp_tz,
		col_uuid, col_json, col_null_int
	FROM type_showcase WHERE id IN (1,2,3) ORDER BY id`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	type row struct {
		id                    int64
		tiny, small, i, big   sql.NullInt64
		real, double          sql.NullFloat64
		decimal, decfloat     sql.NullString
		boolT, boolF          sql.NullBool
		varchar, char, charIC sql.NullString
		binary, varbinary     []byte
		clob                  sql.NullString
		blob                  []byte
		date, timeV           sql.NullTime
		timeTZ, ts, tsTZ      sql.NullTime
		uuid                  sql.NullString
		json                  []byte
		nullInt               sql.NullInt64
	}
	scanRow := func(rows *sql.Rows) *row {
		t.Helper()
		r := &row{}
		err := rows.Scan(&r.id, &r.tiny, &r.small, &r.i, &r.big,
			&r.real, &r.double, &r.decimal, &r.decfloat,
			&r.boolT, &r.boolF,
			&r.varchar, &r.char, &r.charIC,
			&r.binary, &r.varbinary,
			&r.clob, &r.blob,
			&r.date, &r.timeV, &r.timeTZ, &r.ts, &r.tsTZ,
			&r.uuid, &r.json, &r.nullInt)
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
		return r
	}

	// Row 1: typical / maximum values
	if !rows.Next() {
		t.Fatal("expected row 1")
	}
	r1 := scanRow(rows)
	if r1.id != 1 {
		t.Fatalf("row 1: id got %d", r1.id)
	}
	if r1.tiny.Int64 != 127 || r1.small.Int64 != 32767 ||
		r1.i.Int64 != 2147483647 || r1.big.Int64 != 9223372036854775807 {
		t.Errorf("row 1 integers: %d/%d/%d/%d", r1.tiny.Int64, r1.small.Int64, r1.i.Int64, r1.big.Int64)
	}
	if r1.real.Float64 < 3.13 || r1.real.Float64 > 3.15 {
		t.Errorf("row 1 REAL: got %v, want ~3.14", r1.real.Float64)
	}
	if r1.double.Float64 != 2.718281828459045 {
		t.Errorf("row 1 DOUBLE: got %v", r1.double.Float64)
	}
	if r1.decimal.String != "12345.67890" {
		t.Errorf("row 1 DECIMAL: got %q", r1.decimal.String)
	}
	// DECFLOAT decodes as string; exact rendering is H2's toString form.
	if !r1.decfloat.Valid || !strings.HasPrefix(r1.decfloat.String, "3.14159265358979") {
		t.Errorf("row 1 DECFLOAT: got %q (valid=%v)", r1.decfloat.String, r1.decfloat.Valid)
	}
	if !r1.boolT.Bool || r1.boolF.Bool {
		t.Errorf("row 1 BOOLEAN: t=%v f=%v", r1.boolT.Bool, r1.boolF.Bool)
	}
	if r1.varchar.String != "hello, world" {
		t.Errorf("row 1 VARCHAR: got %q", r1.varchar.String)
	}
	if r1.char.String != "CHAR      " {
		t.Errorf("row 1 CHAR: got %q (want 10-char padded)", r1.char.String)
	}
	if r1.charIC.String != "Mixed CASE value" {
		t.Errorf("row 1 VARCHAR_IGNORECASE: got %q", r1.charIC.String)
	}
	if fmt.Sprintf("%X", r1.binary) != "DEADBEEF" {
		t.Errorf("row 1 BINARY: got %X", r1.binary)
	}
	if fmt.Sprintf("%X", r1.varbinary) != "CAFEBABE01020304" {
		t.Errorf("row 1 VARBINARY: got %X", r1.varbinary)
	}
	// Inline CLOB regression (MATURITY_MVP finding 1): must decode as string.
	if !r1.clob.Valid || r1.clob.String != "large clob content for testing" {
		t.Errorf("row 1 CLOB: got %q (valid=%v)", r1.clob.String, r1.clob.Valid)
	}
	if fmt.Sprintf("%X", r1.blob) != "0102030405060708" {
		t.Errorf("row 1 BLOB: got %X", r1.blob)
	}
	if r1.date.Time.Year() != 2024 || r1.date.Time.Month() != 1 || r1.date.Time.Day() != 15 {
		t.Errorf("row 1 DATE: got %v", r1.date.Time)
	}
	if r1.timeV.Time.Hour() != 13 || r1.timeV.Time.Minute() != 45 {
		t.Errorf("row 1 TIME: got %v", r1.timeV.Time)
	}
	if _, off := r1.timeTZ.Time.Zone(); off != 2*3600 {
		t.Errorf("row 1 TIME_TZ offset: got %d, want 7200", off)
	}
	if r1.ts.Time.Hour() != 13 || r1.ts.Time.Minute() != 45 {
		t.Errorf("row 1 TIMESTAMP: got %v", r1.ts.Time)
	}
	if _, off := r1.tsTZ.Time.Zone(); off != 2*3600 {
		t.Errorf("row 1 TIMESTAMP_TZ offset: got %d, want 7200", off)
	}
	if r1.uuid.String != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("row 1 UUID: got %q", r1.uuid.String)
	}
	// JSON now decodes as []byte.
	if len(r1.json) == 0 {
		t.Errorf("row 1 JSON: got %s (len=%d)", string(r1.json), len(r1.json))
	} else {
		t.Logf("row 1 JSON: %s", string(r1.json))
	}
	if r1.nullInt.Valid {
		t.Errorf("row 1 col_null_int: expected NULL, got %d", r1.nullInt.Int64)
	}

	// Row 2: minimum / edge / zero values
	if !rows.Next() {
		t.Fatal("expected row 2")
	}
	r2 := scanRow(rows)
	if r2.id != 2 {
		t.Fatalf("row 2: id got %d", r2.id)
	}
	if r2.tiny.Int64 != -128 || r2.small.Int64 != -32768 ||
		r2.i.Int64 != -2147483648 || r2.big.Int64 != -9223372036854775807 {
		t.Errorf("row 2 integers: %d/%d/%d/%d", r2.tiny.Int64, r2.small.Int64, r2.i.Int64, r2.big.Int64)
	}
	// Empty CLOB must decode as empty string (valid), not NULL.
	if !r2.clob.Valid || r2.clob.String != "" {
		t.Errorf("row 2 CLOB: got %q (valid=%v), want empty string", r2.clob.String, r2.clob.Valid)
	}
	if !bytes.Equal(r2.blob, []byte{0x00}) {
		t.Errorf("row 2 BLOB: got %X, want 00", r2.blob)
	}
	if !bytes.Equal(r2.binary, []byte{0, 0, 0, 0}) {
		t.Errorf("row 2 BINARY: got %X, want 00000000", r2.binary)
	}
	if r2.varchar.Valid && r2.varchar.String != "" {
		t.Errorf("row 2 VARCHAR: got %q, want empty", r2.varchar.String)
	}
	if r2.uuid.String != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("row 2 UUID: got %q", r2.uuid.String)
	}

	// Row 3: every nullable column is NULL
	if !rows.Next() {
		t.Fatal("expected row 3")
	}
	r3 := scanRow(rows)
	if r3.id != 3 {
		t.Fatalf("row 3: id got %d", r3.id)
	}
	if r3.tiny.Valid || r3.small.Valid || r3.i.Valid || r3.big.Valid ||
		r3.real.Valid || r3.double.Valid || r3.decimal.Valid || r3.decfloat.Valid ||
		r3.boolT.Valid || r3.boolF.Valid ||
		r3.varchar.Valid || r3.char.Valid || r3.charIC.Valid ||
		r3.binary != nil || r3.varbinary != nil ||
		r3.clob.Valid || r3.blob != nil ||
		r3.date.Valid || r3.timeV.Valid || r3.timeTZ.Valid || r3.ts.Valid || r3.tsTZ.Valid ||
		r3.uuid.Valid || r3.nullInt.Valid || r3.json != nil {
		t.Errorf("row 3: expected all NULL, got %+v", r3)
	}

	if rows.Next() {
		t.Fatal("unexpected 4th row")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	t.Log("type_showcase full-select matrix passed")
}

// TestIntegration_ComplexTypeDecoding verifies that ENUM, INTERVAL, ARRAY,
// and ROW values decode to their exact documented representations.
// MATURITY_ROUND_II_PLAN.md Task 8 (finding 9): these are golden-string
// assertions, not Contains checks — a formatting regression must fail here.
// The ARRAY NULL-element rendering "<nil>" is pinned by this test (Task 8 owns
// the behavior contract); Task 9 documents it in the README.
func TestIntegration_ComplexTypeDecoding(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	t.Run("ENUM ordinal", func(t *testing.T) {
		var v int64
		err := db.QueryRowContext(ctx,
			"SELECT CAST('SMALL' AS ENUM('SMALL', 'MEDIUM', 'LARGE'))").Scan(&v)
		if err != nil {
			t.Fatalf("ENUM query/scan: %v", err)
		}
		if v != 1 {
			t.Errorf("ENUM: got %d, want 1 (SMALL)", v)
		}
	})

	t.Run("INTERVAL canonical text", func(t *testing.T) {
		// Representative subset covering positive, negative, fractional and
		// zero-padded forms; the full matrix lives in
		// TestIntegration_IntervalCanonicalMatrix (Task 2).
		tests := []struct {
			expr   string
			golden string
		}{
			{"INTERVAL '1-2' YEAR TO MONTH", "INTERVAL '1-2' YEAR TO MONTH"},
			{"INTERVAL '1 02:03:04.5' DAY TO SECOND", "INTERVAL '1 02:03:04.5' DAY TO SECOND"},
			{"INTERVAL '0 00:00:00' DAY TO SECOND", "INTERVAL '0 00:00:00' DAY TO SECOND"},
			{"-INTERVAL '2:03:04.5' HOUR TO SECOND", "INTERVAL '-2:03:04.5' HOUR TO SECOND"},
		}
		for _, tc := range tests {
			var got string
			if err := db.QueryRowContext(ctx, "SELECT "+tc.expr).Scan(&got); err != nil {
				t.Errorf("%s: query/scan: %v", tc.expr, err)
				continue
			}
			if got != tc.golden {
				t.Errorf("%s: decoded %q, want %q", tc.expr, got, tc.golden)
			}
		}
	})

	t.Run("ARRAY exact rendering", func(t *testing.T) {
		var arr string
		err := db.QueryRowContext(ctx, "SELECT ARRAY[1, 2, 3]").Scan(&arr)
		if err != nil {
			t.Fatalf("ARRAY query/scan: %v", err)
		}
		if arr != "[1,2,3]" {
			t.Errorf("ARRAY: got %q, want [1,2,3]", arr)
		}

		// NULL elements render as <nil>: pinned behavior contract.
		err = db.QueryRowContext(ctx, "SELECT ARRAY['a', 'b', 'c', NULL]").Scan(&arr)
		if err != nil {
			t.Fatalf("ARRAY-with-NULL query/scan: %v", err)
		}
		if arr != "[a,b,c,<nil>]" {
			t.Errorf("ARRAY with NULL: got %q, want [a,b,c,<nil>]", arr)
		}
	})

	t.Run("ROW exact rendering", func(t *testing.T) {
		var r string
		err := db.QueryRowContext(ctx, "SELECT (1, 'hello')").Scan(&r)
		if err != nil {
			t.Fatalf("ROW query/scan: %v", err)
		}
		if r != "(1,hello)" {
			t.Errorf("ROW: got %q, want (1,hello)", r)
		}
	})
}

// TestIntegration_FetchOnDemandLOB verifies that large BLOB and CLOB values
// exceeding the inline threshold are fetched via LOB_READ requests, and —
// after the Task 1 fix — regardless of where the LOB appears in the result
// batch: followed by more columns, more rows, or across fetch-size boundaries
// (MATURITY_ROUND_II_PLAN.md Task 1, finding 1).
func TestIntegration_FetchOnDemandLOB(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	// Create a table with LOB columns and insert values larger than the
	// inline threshold (H2's default MAX_LENGTH_INPLACE_LOB is 256 bytes for
	// CLOB).
	table := integrationTxTableName("lob_demand")
	_, err := db.ExecContext(ctx, `CREATE TABLE `+table+` (
		id INT PRIMARY KEY,
		small_clob CLOB,
		large_clob CLOB,
		large_blob BLOB
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	// Distinct payloads, each above the inline threshold.
	clobA := strings.Repeat("Hello World! ", 38)            // 494 chars
	clobB := strings.Repeat("Second Row CLOB ", 32)         // 512 chars
	clobC := strings.Repeat("Two LOB Columns Here ", 24)    // 504 chars
	clobD := strings.Repeat("Mixed Inline And Demand ", 22) // 528 chars
	smallClob := "tiny inline clob"
	blobC := bytes.Repeat([]byte{0xAB, 0xCD}, 250) // 500 bytes

	inserts := []struct {
		id           int
		small, large string
		blob         []byte
	}{
		{1, "", clobA, nil},
		{2, "", clobB, nil},
		{3, "", clobC, blobC},      // both LOB columns large: the "status 15" shape
		{4, smallClob, clobD, nil}, // inline + on-demand in one row
	}
	for _, ins := range inserts {
		_, err := db.ExecContext(ctx,
			"INSERT INTO "+table+" (id, small_clob, large_clob, large_blob) VALUES (?, ?, ?, ?)",
			ins.id, ins.small, ins.large, ins.blob)
		if err != nil {
			t.Fatalf("INSERT id=%d: %v", ins.id, err)
		}
	}

	t.Run("single column single row regression", func(t *testing.T) {
		var clobVal string
		err := db.QueryRowContext(ctx, "SELECT large_clob FROM "+table+" WHERE id = 1").Scan(&clobVal)
		if err != nil {
			t.Fatalf("SELECT CLOB: %v", err)
		}
		if clobVal != clobA {
			t.Errorf("CLOB mismatch: got %d chars", len(clobVal))
		}

		var blobVal []byte
		err = db.QueryRowContext(ctx, "SELECT large_blob FROM "+table+" WHERE id = 3").Scan(&blobVal)
		if err != nil {
			t.Fatalf("SELECT BLOB: %v", err)
		}
		if !bytes.Equal(blobVal, blobC) {
			t.Errorf("BLOB: got %d bytes, want %d", len(blobVal), len(blobC))
		}
	})

	// Shape: LOB column followed by another LOB column in the same row.
	// Before the fix this failed with "unexpected status 15".
	t.Run("two LOB columns in one row", func(t *testing.T) {
		var gotClob string
		var gotBlob []byte
		err := db.QueryRowContext(ctx,
			"SELECT large_clob, large_blob FROM "+table+" WHERE id = 3").Scan(&gotClob, &gotBlob)
		if err != nil {
			t.Fatalf("SELECT two LOB columns: %v", err)
		}
		if gotClob != clobC {
			t.Errorf("CLOB mismatch (%d chars)", len(gotClob))
		}
		if !bytes.Equal(gotBlob, blobC) {
			t.Errorf("BLOB mismatch: got %d bytes, want %d", len(gotBlob), len(blobC))
		}
	})

	// Shape: multiple rows of one CLOB column. Before the fix this failed
	// with "unexpected status 16777216" when parsing row 2.
	t.Run("multiple rows of one CLOB column", func(t *testing.T) {
		want := map[int]string{1: clobA, 2: clobB, 3: clobC, 4: clobD}
		rows, err := db.QueryContext(ctx,
			"SELECT id, large_clob FROM "+table+" WHERE id <= 4 ORDER BY id")
		if err != nil {
			t.Fatalf("SELECT multi-row CLOB: %v", err)
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var id int
			var got string
			if err := rows.Scan(&id, &got); err != nil {
				t.Fatalf("Scan row %d: %v", seen, err)
			}
			if got != want[id] {
				t.Errorf("row %d CLOB mismatch: got %d chars, want %d", id, len(got), len(want[id]))
			}
			seen++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Err: %v", err)
		}
		if seen != 4 {
			t.Errorf("saw %d rows, want 4", seen)
		}
	})

	// Shape: LOB followed by a scalar column.
	t.Run("LOB followed by scalar column", func(t *testing.T) {
		var got string
		var id int
		err := db.QueryRowContext(ctx,
			"SELECT large_clob, id FROM "+table+" WHERE id = 1").Scan(&got, &id)
		if err != nil {
			t.Fatalf("SELECT CLOB + scalar: %v", err)
		}
		if got != clobA || id != 1 {
			t.Errorf("got %d chars / id=%d", len(got), id)
		}
	})

	// Shape: inline LOB and on-demand LOB in one row.
	t.Run("mixed inline and on-demand LOBs", func(t *testing.T) {
		var gotLarge, gotSmall string
		err := db.QueryRowContext(ctx,
			"SELECT large_clob, small_clob FROM "+table+" WHERE id = 4").Scan(&gotLarge, &gotSmall)
		if err != nil {
			t.Fatalf("SELECT mixed LOBs: %v", err)
		}
		if gotLarge != clobD || gotSmall != smallClob {
			t.Errorf("mixed row mismatch: large=%d chars small=%q", len(gotLarge), gotSmall)
		}
	})

	// Shape: batch boundary crossing — one row per RESULT_FETCH_ROWS batch,
	// so deferred LOBs must resolve at several successive mid-result
	// boundaries. Uses a dedicated handle with Config.FetchSize = 1 (there is
	// no per-statement fetch size).
	t.Run("fetch size one crosses every boundary", func(t *testing.T) {
		cfg, err := ParseDSN(env["JDBC_URL"])
		if err != nil {
			t.Fatalf("ParseDSN: %v", err)
		}
		MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
		cfg.FetchSize = 1
		batchDB, err := OpenDB(cfg)
		if err != nil {
			t.Fatalf("OpenDB(fetchSize=1): %v", err)
		}
		defer batchDB.Close()

		want := map[int]string{1: clobA, 2: clobB, 3: clobC, 4: clobD}
		rows, err := batchDB.QueryContext(ctx,
			"SELECT id, large_clob FROM "+table+" ORDER BY id")
		if err != nil {
			t.Fatalf("batched SELECT: %v", err)
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var id int
			var got string
			if err := rows.Scan(&id, &got); err != nil {
				t.Fatalf("Scan batched row %d: %v", seen, err)
			}
			if got != want[id] {
				t.Errorf("batched row %d CLOB mismatch: got %d chars", id, len(got))
			}
			seen++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Err: %v", err)
		}
		if seen != 4 {
			t.Errorf("saw %d rows, want 4", seen)
		}

		// Pool sanity: the same handle must keep serving correct results.
		var probe int
		if err := batchDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&probe); err != nil || probe != 4 {
			t.Errorf("post-streaming query: count=%d err=%v, want 4/nil", probe, err)
		}
	})
}

// TestIntegration_GeneratedKeysWithLob verifies that a generated-keys frame
// containing a fetch-on-demand LOB resolves the value correctly (Task 1).
func TestIntegration_GeneratedKeysWithLob(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}

	// Request the DOC column explicitly so the inserted CLOB value is part of
	// the generated-keys result.
	cfg, err := ParseDSN(env["JDBC_URL"])
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
	cfg.GeneratedKeysMode = GeneratedKeysColumnNames
	cfg.GeneratedKeysColumnNames = []string{"DOC"}
	cfg.GeneratedKeysModeSet = true

	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	table := integrationTxTableName("genkeys_lob")
	_, err = db.ExecContext(ctx, `CREATE TABLE `+table+` (
		id BIGINT GENERATED BY DEFAULT AS IDENTITY,
		doc CLOB
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table) })

	// Above the inline threshold: the value arrives as fetch-on-demand.
	bigDoc := strings.Repeat("Generated DOC ", 80) // 1120 chars

	rawConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer rawConn.Close()

	var h2Res *result
	err = rawConn.Raw(func(raw any) error {
		c := raw.(*conn)
		driverRes, execErr := c.ExecContext(ctx, "INSERT INTO "+table+" (doc) VALUES (?)", []driver.NamedValue{
			{Ordinal: 1, Value: bigDoc},
		})
		if execErr != nil {
			return execErr
		}
		var ok bool
		h2Res, ok = driverRes.(*result)
		if !ok {
			return errors.New("ExecContext did not return *h2go.result")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("INSERT with generated keys: %v", err)
	}

	gk := h2Res.GeneratedKeys
	if gk == nil || len(gk.Rows) != 1 {
		t.Fatalf("generated keys missing or wrong shape: %+v", gk)
	}
	if len(gk.Columns) == 0 || gk.Columns[0] != "DOC" {
		t.Errorf("generated key columns = %v, want [DOC]", gk.Columns)
	}
	gotDoc, ok := gk.Rows[0][0].(string)
	if !ok {
		t.Fatalf("generated key type = %T, want string", gk.Rows[0][0])
	}
	if gotDoc != bigDoc {
		t.Errorf("generated LOB mismatch: got %d chars, want %d", len(gotDoc), len(bigDoc))
	}
}

// TestIntegration_IntervalCanonicalMatrix compares the driver's INTERVAL
// decoding against H2's own canonical text (CAST(... AS VARCHAR)) for every
// qualifier, live (MATURITY_ROUND_II_PLAN.md Task 2, finding 3).
func TestIntegration_IntervalCanonicalMatrix(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	tests := []struct {
		expr   string
		golden string
	}{
		{"INTERVAL '5.25' SECOND", "INTERVAL '5.25' SECOND"},
		{"INTERVAL '7.750000000' SECOND", "INTERVAL '7.75' SECOND"},
		{"INTERVAL '7' SECOND", "INTERVAL '7' SECOND"},
		{"-INTERVAL '5.25' SECOND", "INTERVAL '-5.25' SECOND"},
		{"INTERVAL '1 02:03:04.5' DAY TO SECOND", "INTERVAL '1 02:03:04.5' DAY TO SECOND"},
		{"INTERVAL '1 02:03:04.000000001' DAY TO SECOND", "INTERVAL '1 02:03:04.000000001' DAY TO SECOND"},
		{"INTERVAL '0 00:00:00' DAY TO SECOND", "INTERVAL '0 00:00:00' DAY TO SECOND"},
		{"-INTERVAL '1 02:03:04.5' DAY TO SECOND", "INTERVAL '-1 02:03:04.5' DAY TO SECOND"},
		{"INTERVAL '23:59:59.999999999' HOUR TO SECOND", "INTERVAL '23:59:59.999999999' HOUR TO SECOND"},
		{"-INTERVAL '2:03:04.5' HOUR TO SECOND", "INTERVAL '-2:03:04.5' HOUR TO SECOND"},
		{"INTERVAL '100:03:04.567890123' HOUR TO SECOND", "INTERVAL '100:03:04.567890123' HOUR TO SECOND"},
		{"INTERVAL '3:04.5' MINUTE TO SECOND", "INTERVAL '3:04.5' MINUTE TO SECOND"},
		{"INTERVAL '2 3' DAY TO HOUR", "INTERVAL '2 03' DAY TO HOUR"},
		{"-INTERVAL '2 3:05' DAY TO MINUTE", "INTERVAL '-2 03:05' DAY TO MINUTE"},
		{"INTERVAL '2:03' HOUR TO MINUTE", "INTERVAL '2:03' HOUR TO MINUTE"},
		{"INTERVAL '0-1' YEAR TO MONTH", "INTERVAL '0-1' YEAR TO MONTH"},
		{"-INTERVAL '1-6' YEAR TO MONTH", "INTERVAL '-1-6' YEAR TO MONTH"},
		{"INTERVAL '42' DAY", "INTERVAL '42' DAY"},
	}

	for _, tc := range tests {
		var got string
		if err := db.QueryRowContext(ctx,
			"SELECT CAST("+tc.expr+" AS VARCHAR)").Scan(&got); err != nil {
			t.Errorf("%s: query: %v", tc.expr, err)
			continue
		}
		if got != tc.golden {
			t.Errorf("%s: driver decoded %q, H2 canonical is %q", tc.expr, got, tc.golden)
		}
	}
}

// TestIntegration_NullDecoding verifies NULL values decode to nil across types.
func TestIntegration_NullDecoding(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	row := db.QueryRowContext(ctx,
		"SELECT CAST(NULL AS INTEGER), CAST(NULL AS VARCHAR), CAST(NULL AS TIMESTAMP)")
	var i sql.NullInt64
	var s sql.NullString
	var ts sql.NullTime
	if err := row.Scan(&i, &s, &ts); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if i.Valid || s.Valid || ts.Valid {
		t.Errorf("expected all NULL, got int=%v str=%v ts=%v", i, s, ts)
	}
	t.Log("null decoding passed")
}

// TestIntegration_ColumnTypes verifies the database/sql ColumnTypes metadata
// mirrors the H2 wire metadata for names, types, lengths, precision/scale, and
// scan type hints.
func TestIntegration_ColumnTypes(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	table := integrationTxTableName("metadata")
	_, err := db.ExecContext(ctx, `CREATE TABLE `+table+` (
		id BIGINT PRIMARY KEY,
		active BOOLEAN NOT NULL,
		name VARCHAR(42),
		amount NUMERIC(12,4),
		created TIMESTAMP(6),
		payload VARBINARY(16),
		uid UUID
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table) }()

	rows, err := db.QueryContext(ctx, "SELECT id, active, name, amount, created, payload, uid FROM "+table+" WHERE 1=0")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	cols, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("ColumnTypes failed: %v", err)
	}
	if len(cols) != 7 {
		t.Fatalf("len(ColumnTypes)=%d, want 7", len(cols))
	}

	tests := []struct {
		name      string
		wantType  string
		wantScan  reflect.Type
		wantLen   int64
		wantPrec  int64
		wantScale int64
	}{
		{"ID", "BIGINT", reflect.TypeOf(int64(0)), 0, 0, 0},
		{"ACTIVE", "BOOLEAN", reflect.TypeOf(true), 0, 0, 0},
		{"NAME", "VARCHAR", reflect.TypeOf(""), 42, 0, 0},
		{"AMOUNT", "NUMERIC", reflect.TypeOf(""), 0, 12, 4},
		{"CREATED", "TIMESTAMP", reflect.TypeOf(time.Time{}), 0, 0, 6},
		{"PAYLOAD", "VARBINARY", reflect.TypeOf([]byte(nil)), 16, 0, 0},
		{"UID", "UUID", reflect.TypeOf(""), 0, 0, 0},
	}

	for i, tc := range tests {
		ct := cols[i]
		if got := ct.Name(); got != tc.name {
			t.Fatalf("column %d name=%q, want %q", i, got, tc.name)
		}
		if got := ct.DatabaseTypeName(); got != tc.wantType {
			t.Fatalf("column %d type=%q, want %q", i, got, tc.wantType)
		}
		if got := ct.ScanType(); got != tc.wantScan {
			t.Fatalf("column %d scan type=%v, want %v", i, got, tc.wantScan)
		}
		if tc.wantLen > 0 {
			gotLen, ok := ct.Length()
			if !ok || gotLen != tc.wantLen {
				t.Fatalf("column %d length=(%d,%v), want (%d,true)", i, gotLen, ok, tc.wantLen)
			}
		}
		if tc.wantPrec > 0 || tc.wantScale > 0 {
			prec, scale, ok := ct.DecimalSize()
			if !ok || prec != tc.wantPrec || scale != tc.wantScale {
				t.Fatalf("column %d DecimalSize=(%d,%d,%v), want (%d,%d,true)", i, prec, scale, ok, tc.wantPrec, tc.wantScale)
			}
		}
		nullable, ok := ct.Nullable()
		if ok {
			t.Logf("column %d nullable=%v", i, nullable)
		} else {
			t.Logf("column %d nullable unavailable", i)
		}
	}
}

// integrationTxTableName returns a unique, unquoted table name for transaction tests.
func integrationTxTableName(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// TestIntegration_TransactionCommit verifies that a transaction commit persists
// changes and returns the connection to autocommit mode.
func TestIntegration_TransactionCommit(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	table := integrationTxTableName("tx_commit")
	if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY, note VARCHAR(64))"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (id, note) VALUES (?, ?)", 1, "committed"); err != nil {
		t.Fatalf("tx.ExecContext: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var note string
	if err := conn.QueryRowContext(ctx, "SELECT note FROM "+table+" WHERE id = 1").Scan(&note); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if note != "committed" {
		t.Fatalf("note = %q, want committed", note)
	}
}

// TestIntegration_TransactionRollback verifies that rollback discards changes.
func TestIntegration_TransactionRollback(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	table := integrationTxTableName("tx_rollback")
	if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY, note VARCHAR(64))"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (id, note) VALUES (?, ?)", 1, "rolled back"); err != nil {
		t.Fatalf("tx.ExecContext: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id = 1").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 after rollback", count)
	}
}

// TestIntegration_TransactionNestedBeginRejected verifies the same raw
// connection cannot start a second transaction while one is already open.
func TestIntegration_TransactionNestedBeginRejected(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("conn.BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := conn.BeginTx(ctx, nil); err == nil {
		t.Fatal("expected error when starting a nested transaction on the same connection")
	}
}

// TestIntegration_ResetSessionRollsBackPendingTransaction verifies that a dirty
// pooled connection is reset before reuse, so uncommitted work is not visible
// to the next borrower.
func TestIntegration_ResetSessionRollsBackPendingTransaction(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()

	table := integrationTxTableName("reset_session")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY, note VARCHAR(64))"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	})

	sqlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}

	var beforeSessionID string
	err = sqlConn.Raw(func(raw any) error {
		c := raw.(*conn)
		beforeSessionID = c.sess.id
		if _, err := c.BeginTx(ctx, driver.TxOptions{}); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "INSERT INTO "+table+" (id, note) VALUES (1, 'dirty')", nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("raw tx setup: %v", err)
	}

	if err := sqlConn.Close(); err != nil {
		t.Fatalf("sql.Conn.Close: %v", err)
	}

	reusedConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn after reset: %v", err)
	}
	defer func() { _ = reusedConn.Close() }()

	var afterSessionID string
	if err := reusedConn.Raw(func(raw any) error {
		afterSessionID = raw.(*conn).sess.id
		return nil
	}); err != nil {
		t.Fatalf("reusedConn.Raw: %v", err)
	}
	if afterSessionID != beforeSessionID {
		t.Fatalf("session ID changed across pool reuse: before=%s after=%s", beforeSessionID[:8], afterSessionID[:8])
	}

	var count int
	if err := reusedConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id = 1").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 after ResetSession rollback", count)
	}
}

// TestIntegration_ValidatorReportsLiveSession verifies IsValid succeeds on a
// live session and round-trips to the server without changing state.
func TestIntegration_ValidatorReportsLiveSession(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	sqlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()

	var valid bool
	if err := sqlConn.Raw(func(raw any) error {
		valid = raw.(*conn).IsValid()
		return nil
	}); err != nil {
		t.Fatalf("conn.Raw: %v", err)
	}
	if !valid {
		t.Fatal("expected IsValid to report true for a live session")
	}
}

// TestIntegration_ConnectionPoolStress exercises repeated pool acquisition and
// release across several goroutines to catch reuse/cleanup regressions under
// load. It intentionally keeps the workload small so it remains fast in local
// integration runs while still hitting the driver pool hooks many times.
func TestIntegration_ConnectionPoolStress(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	// Generous ceilings: this test verifies pool-safety and concurrency
	// correctness, not latency. Under -race on shared CI runners (2 vCPU,
	// alongside the Java servers) round trips can be an order of magnitude
	// slower than local runs.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const workers = 4
	const iterations = 6
	errCh := make(chan error, workers*iterations)
	var wg sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				iterCtx, iterCancel := context.WithTimeout(ctx, 20*time.Second)
				conn, err := db.Conn(iterCtx)
				if err != nil {
					iterCancel()
					errCh <- fmt.Errorf("worker %d iter %d db.Conn: %w", worker, iter, err)
					return
				}

				var got int64
				err = conn.QueryRowContext(iterCtx, "SELECT 1").Scan(&got)
				if err == nil && got != 1 {
					err = fmt.Errorf("worker %d iter %d SELECT 1 got %d, want 1", worker, iter, got)
				}
				closeErr := conn.Close()
				iterCancel()
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d query: %w", worker, iter, err)
					return
				}
				if closeErr != nil {
					errCh <- fmt.Errorf("worker %d iter %d conn.Close: %w", worker, iter, closeErr)
					return
				}
			}
		}(worker)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
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

// TestIntegration_SQLError verifies that a real H2 SQL error (syntax error)
// is decoded into a structured *Error with a non-zero SQLState and Code.
func TestIntegration_SQLError(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)

	// Execute deliberately invalid SQL — H2 returns error code 42001 (syntax error).
	_, err := db.ExecContext(context.Background(), "SELECT * FORM not_a_table WHERE !!")
	if err == nil {
		t.Fatal("expected SQL syntax error, got nil")
	}

	// The error must be reachable as *Error via errors.As so callers can inspect
	// SQLState and Code without string parsing.
	var h2err *Error
	if !errors.As(err, &h2err) {
		t.Fatalf("expected *Error via errors.As, got %T: %v", err, err)
	}
	if h2err.SQLState == "" {
		t.Errorf("SQLState is empty — server error not decoded")
	}
	if h2err.Code == 0 {
		t.Errorf("Code is 0 — server error code not decoded")
	}
	t.Logf("SQL error: SQLState=%s Code=%d Message=%s", h2err.SQLState, h2err.Code, h2err.Message)
}

// TestIntegration_DecfloatExactRoundTrip verifies exact DECFLOAT handling end
// to end (post-v0.2.0 backlog item #3): the wire string decoded by the driver
// must equal H2's own canonical rendering byte-for-byte for finite values
// (including scientific-notation normalization), the special values
// Infinity/-Infinity/NaN must round-trip, DecFloat.String() must reproduce
// H2's text exactly, and a bound *DecFloat must survive an insert/read cycle.
func TestIntegration_DecfloatExactRoundTrip(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS dec_probe"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE dec_probe") }()
	if _, err := db.ExecContext(ctx, "CREATE TABLE dec_probe(id INT PRIMARY KEY, v DECFLOAT)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// in is bound as a plain string parameter; want is H2's canonical text
	// (BigDecimal.toString semantics), verified against both the raw driver
	// decode and CAST(v AS VARCHAR).
	//
	// Note the normalization H2 applies when coercing VARCHAR to DECFLOAT on
	// assignment: insignificant trailing zeros are stripped (12134567890E+3 →
	// 1.213456789E+13, and a 40-digit literal ending in 0 loses that zero),
	// and zero collapses to plain "0" regardless of its source scale. The
	// stored value is what getString() then puts on the wire, so these
	// goldens pin the post-normalization forms.
	literals := []struct{ in, want string }{
		{"123.456", "123.456"},
		{"-123.456", "-123.456"},
		{"0.001", "0.001"},
		{"0.00", "0"},
		{"000123", "123"},
		{"5.", "5"},
		{".25", "0.25"},
		{"1E+7", "1E+7"},
		{"1e7", "1E+7"},
		{"1E-7", "1E-7"},
		{"1.5e-25", "1.5E-25"},
		{"123E-5", "0.00123"},
		{"12134567890E+3", "1.213456789E+13"},
		{"1234567890123456789012345678901234567890",
			"1.23456789012345678901234567890123456789E+39"},
	}
	for i, tc := range literals {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO dec_probe VALUES (?, ?)", i+1, tc.in); err != nil {
			t.Fatalf("insert %q: %v", tc.in, err)
		}
		var raw, casted string
		if err := db.QueryRowContext(ctx,
			"SELECT v, CAST(v AS VARCHAR) FROM dec_probe WHERE id = ?", i+1).
			Scan(&raw, &casted); err != nil {
			t.Fatalf("read %q: %v", tc.in, err)
		}
		if raw != tc.want {
			t.Errorf("%q: driver decoded %q, want %q", tc.in, raw, tc.want)
		}
		if casted != tc.want {
			t.Errorf("%q: server renders %q via CAST, want %q", tc.in, casted, tc.want)
		}
		df, perr := ParseDecFloat(raw)
		if perr != nil {
			t.Errorf("%q: ParseDecFloat: %v", raw, perr)
			continue
		}
		if got := df.String(); got != tc.want {
			t.Errorf("%q: DecFloat.String() = %q, want %q", raw, got, tc.want)
		}
	}

	t.Run("special values", func(t *testing.T) {
		tests := []struct {
			expr   string
			isInf  int // math.IsInf sign convention; 0 means NaN
			golden string
		}{
			{"CAST('Infinity' AS DECFLOAT)", 1, "Infinity"},
			{"CAST('-Infinity' AS DECFLOAT)", -1, "-Infinity"},
			{"CAST('NaN' AS DECFLOAT)", 0, "NaN"},
		}
		for _, tc := range tests {
			var df DecFloat
			if err := db.QueryRowContext(ctx, "SELECT "+tc.expr).Scan(&df); err != nil {
				t.Errorf("%s: scan into DecFloat: %v", tc.expr, err)
				continue
			}
			if got := df.String(); got != tc.golden {
				t.Errorf("%s: String() = %q, want %q", tc.expr, got, tc.golden)
			}
			if tc.isInf == 0 {
				if !df.IsNaN() {
					t.Errorf("%s: expected NaN", tc.expr)
				}
			} else if !df.IsInf(tc.isInf) {
				t.Errorf("%s: expected infinity sign %d", tc.expr, tc.isInf)
			}
		}
	})

	t.Run("bound DecFloat write path", func(t *testing.T) {
		df, err := ParseDecFloat("-98765.4321")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO dec_probe VALUES (?, ?)", len(literals)+100, df); err != nil {
			t.Fatalf("insert with bound DecFloat: %v", err)
		}
		var back DecFloat
		if err := db.QueryRowContext(ctx,
			"SELECT v FROM dec_probe WHERE id = ?", len(literals)+100).Scan(&back); err != nil {
			t.Fatalf("read: %v", err)
		}
		if !back.IsFinite() || back.Scale() != 4 || back.UnscaledInt().String() != "-987654321" {
			t.Errorf("round trip mismatch: %q scale=%d unscaled=%v", back.String(), back.Scale(), back.UnscaledInt())
		}
	})
}

// TestIntegration_TLSTransport verifies TLS transport against a live H2
// server started with -tcpSSL (post-v0.2.0 backlog item #4).
//
// Requires:
//   - JDBC_TLS_URL pointing at the TLS listener, e.g.
//     jdbc:h2:ssl://localhost:9093/h2-go  (start via: cd h2-data && ./h2-tls.sh)
//   - optionally h2-data/tls/cert.pem (generated by gen-tls-certs.sh) to pin
//     the self-signed test certificate as a trusted root; without it the
//     test falls back to TLSInsecureSkipVerify and logs a note.
//
// Skips cleanly when either is unavailable so the default matrix is
// unaffected.
func TestIntegration_TLSTransport(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	tlsURL := os.Getenv("JDBC_TLS_URL")
	if tlsURL == "" {
		t.Skip("integration test skipped: JDBC_TLS_URL not set (start h2-data/h2-tls.sh)")
	}

	cfg, err := ParseDSN(tlsURL)
	if err != nil {
		t.Fatalf("ParseDSN(JDBC_TLS_URL): %v", err)
	}
	if !cfg.TLS {
		t.Fatalf("JDBC_TLS_URL must select TLS via the ssl:// scheme, got %q", tlsURL)
	}
	MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])

	if pem, err := os.ReadFile(filepath.FromSlash("h2-data/tls/cert.pem")); err == nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			t.Fatal("could not parse h2-data/tls/cert.pem")
		}
		cfg.TLSRootCAs = pool
	} else {
		t.Log("h2-data/tls/cert.pem not found; using TLSInsecureSkipVerify")
		cfg.TLSInsecureSkipVerify = true
	}

	ctx := context.Background()

	t.Run("ssl scheme handshake and query", func(t *testing.T) {
		db, err := OpenDB(cfg)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		defer db.Close()
		var got int
		if err := db.QueryRowContext(ctx, "SELECT 1+1").Scan(&got); err != nil {
			t.Fatalf("query over TLS: %v", err)
		}
		if got != 2 {
			t.Errorf("SELECT 1+1 = %d, want 2", got)
		}
	})

	t.Run("programmatic TLS flag", func(t *testing.T) {
		cfg2 := *cfg
		cfg2.TLSRootCAs = cfg.TLSRootCAs
		cfg2.Database = "tls-probe-flag"
		db, err := OpenDB(&cfg2)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		defer db.Close()
		var got int
		if err := db.QueryRowContext(ctx, "SELECT 41+1").Scan(&got); err != nil {
			t.Fatalf("query over TLS: %v", err)
		}
		if got != 42 {
			t.Errorf("SELECT 41+1 = %d, want 42", got)
		}
	})

	t.Run("verification rejects untrusted server", func(t *testing.T) {
		bad := *cfg
		bad.Database = "tls-probe-untrusted"
		bad.TLSRootCAs = x509.NewCertPool() // trust nothing
		bad.TLSInsecureSkipVerify = false
		db, err := OpenDB(&bad)
		if err != nil {
			t.Fatalf("OpenDB should succeed lazily: %v", err)
		}
		defer db.Close()
		var got int
		err = db.QueryRowContext(ctx, "SELECT 1").Scan(&got)
		if err == nil {
			t.Fatal("expected certificate verification failure against untrusted server")
		}
		if !strings.Contains(err.Error(), "certificate") {
			t.Errorf("error %q should mention \"certificate\"", err)
		}
	})
}

// TestIntegration_StatementCancellation verifies deep statement cancellation
// (post-v0.2.0 backlog item #5): a context deadline during a long-running
// query fires the side-channel SESSION_CANCEL_STATEMENT, the driver surfaces
// context.DeadlineExceeded, and the SAME connection remains usable because
// the server's aligned cancellation report was consumed instead of the
// session being aborted.
func TestIntegration_StatementCancellation(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	// Pin one pooled connection so the before/after probes provably hit the
	// same session that ran the cancelled statement.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	var one int
	if err := conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("pre-check: %v (got %d)", err, one)
	}

	// A cartesian product far too large to finish: the server must still be
	// executing when the deadline fires.
	queryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, queryErr := conn.QueryContext(queryCtx,
		"SELECT COUNT(*) FROM SYSTEM_RANGE(1, 10000000) A, SYSTEM_RANGE(1, 1000000) B")
	elapsed := time.Since(start)

	if !errors.Is(queryErr, context.DeadlineExceeded) {
		t.Fatalf("cancelled query error = %v, want context.DeadlineExceeded", queryErr)
	}
	t.Logf("query cancelled after %v", elapsed)

	var two int
	if err := conn.QueryRowContext(ctx, "SELECT 2").Scan(&two); err != nil || two != 2 {
		t.Fatalf("post-cancel same-connection query failed: %v (got %d)", err, two)
	}

	// And the pool as a whole stays healthy.
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping after cancellation: %v", err)
	}
}

// TestIntegration_PerStatementGeneratedKeys verifies per-statement
// generated-keys overrides (post-v0.2.0 backlog item #7): a request attached
// to ExecContext's context wins over the connection-level configuration for
// exactly that statement, including suppressing keys entirely, requesting
// specific columns by name at the driver level, and not leaking into
// subsequent statements.
func TestIntegration_PerStatementGeneratedKeys(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	db := integrationDB(t, env)
	ctx := context.Background()

	table := "gk_probe_" + uuid.NewString()[:8]
	if _, err := db.ExecContext(ctx,
		"CREATE TABLE "+table+"(id BIGINT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(100))"); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE "+table) }()

	// 1. Default (auto): LastInsertId available.
	res, err := db.ExecContext(ctx, "INSERT INTO "+table+"(name) VALUES (?)", "auto")
	if err != nil {
		t.Fatalf("default exec: %v", err)
	}
	id1, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("default exec LastInsertId: %v", err)
	}
	if id1 <= 0 {
		t.Errorf("default exec LastInsertId = %d, want > 0", id1)
	}

	// 2. Per-statement suppression on an auto-configured connection.
	suppressed := ContextWithoutGeneratedKeys(ctx)
	res, err = db.ExecContext(suppressed, "INSERT INTO "+table+"(name) VALUES (?)", "none")
	if err != nil {
		t.Fatalf("suppressed exec: %v", err)
	}
	if _, lastErr := res.LastInsertId(); !errors.Is(lastErr, ErrLastInsertIDUnavailable) {
		t.Errorf("suppressed exec LastInsertId error = %v, want ErrLastInsertIDUnavailable", lastErr)
	}

	// 3. Column-names override, observed through the full generated-keys
	// result via sql.Conn.Raw (database/sql wraps plain sql.Result).
	sqlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer sqlConn.Close()
	err = sqlConn.Raw(func(raw any) error {
		execer, ok := raw.(driver.ExecerContext)
		if !ok {
			return errors.New("connection does not implement driver.ExecerContext")
		}
		overrideCtx := ContextWithGeneratedKeys(ctx, GeneratedKeysRequest{
			Mode:  GeneratedKeysColumnNames,
			Names: []string{"NAME"},
		})
		dres, derr := execer.ExecContext(overrideCtx,
			"INSERT INTO "+table+"(name) VALUES (?)",
			[]driver.NamedValue{{Ordinal: 1, Value: "by-name"}})
		if derr != nil {
			return derr
		}
		provider, ok := dres.(GeneratedKeysProvider)
		if !ok {
			return errors.New("result does not expose GeneratedKeysProvider")
		}
		keys := provider.GetGeneratedKeys()
		if keys == nil {
			return errors.New("generated keys result is nil")
		}
		if len(keys.Columns) != 1 || keys.Columns[0] != "NAME" {
			return fmt.Errorf("keys columns = %v, want [NAME]", keys.Columns)
		}
		if len(keys.Rows) != 1 || len(keys.Rows[0]) != 1 {
			return fmt.Errorf("keys rows = %v, want one single-column row", keys.Rows)
		}
		if got, _ := keys.Rows[0][0].(string); got != "by-name" {
			return fmt.Errorf("key value = %v, want by-name", keys.Rows[0][0])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("column-names override: %v", err)
	}

	// 4. No leakage: the next default exec has auto keys again.
	res, err = db.ExecContext(ctx, "INSERT INTO "+table+"(name) VALUES (?)", "after")
	if err != nil {
		t.Fatalf("post-override exec: %v", err)
	}
	if id3, lerr := res.LastInsertId(); lerr != nil || id3 <= 0 {
		t.Errorf("post-override LastInsertId = %d, %v; want auto keys again", id3, lerr)
	}
}
