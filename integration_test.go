//go:build integration

package h2go

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
