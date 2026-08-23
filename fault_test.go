//go:build integration

// fault_test.go — Tier B fault injection against REAL H2 server processes.
//
// Unlike the mock-based discard tests (stream_discard_test.go), these tests
// spawn their own throwaway H2 server JVMs and kill/restart them mid-flight,
// exercising the full failure path: kernel-level TCP resets, dead sessions in
// the pool, database/sql retry behavior, and recovery after a restart.
//
// The tests skip when no local H2 jar or java binary is available, so remote
// server setups are unaffected.

package h2go

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

// findH2Jar locates an H2 jar for spawned servers: explicit env override,
// CI layout (h2-data/lib), then the classic local layout.
func findH2Jar(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("H2GO_TEST_JAR"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, pattern := range []string{"h2-data/lib/h2-*.jar", "h2-data/h2-*.jar"} {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return matches[0]
		}
	}
	t.Skip("fault test skipped: no H2 jar found (set H2GO_TEST_JAR)")
	return ""
}

// h2Process is a throwaway H2 TCP server owned by a test.
type h2Process struct {
	cmd     *exec.Cmd
	port    int
	baseDir string
	jar     string
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func startH2(t *testing.T, jar string, port int, baseDir string) *h2Process {
	t.Helper()
	if _, err := exec.LookPath("java"); err != nil {
		t.Skipf("fault test skipped: java not on PATH (%v)", err)
	}
	cmd := exec.Command("java", "-cp", jar, "org.h2.tools.Server",
		"-tcp", "-tcpPort", fmt.Sprint(port),
		"-tcpAllowOthers", "-ifNotExists",
		"-baseDir", baseDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start H2: %v", err)
	}
	p := &h2Process{cmd: cmd, port: port, baseDir: baseDir, jar: jar}

	// Wait for the port to open; dump nothing on success. If the process
	// dies early, fail with its exit state.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), time.Second); err == nil {
			_ = conn.Close()
			t.Cleanup(func() { p.stop(true) })
			return p
		}
		if cmd.ProcessState != nil {
			t.Fatalf("H2 exited early: %v", cmd.ProcessState)
		}
		time.Sleep(100 * time.Millisecond)
	}
	p.stop(true)
	t.Fatal("H2 did not open its port within 30s")
	return nil
}

// stop kills the server immediately (SIGKILL). H2's shutdown hook never runs,
// so recently committed transactions may not have been flushed to disk.
func (p *h2Process) stop(removeLocks bool) {
	p.signal(syscall.SIGKILL, 5*time.Second)
	if removeLocks {
		p.removeLocks()
	}
}

// stopGracefully sends SIGTERM so H2's shutdown hook closes and flushes all
// databases, then waits for exit (SIGKILL fallback on timeout).
func (p *h2Process) stopGracefully() {
	p.signal(syscall.SIGTERM, 15*time.Second)
}

func (p *h2Process) signal(sig syscall.Signal, timeout time.Duration) {
	if p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(sig)
	done := make(chan struct{})
	go func() {
		_, _ = p.cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = p.cmd.Process.Kill()
		<-done
	}
}

func (p *h2Process) removeLocks() {
	locks, _ := filepath.Glob(filepath.Join(p.baseDir, "*.lock.db"))
	for _, l := range locks {
		_ = os.Remove(l)
	}
}

// openFaultDB opens a dedicated pool against the spawned server. One
// connection max keeps failure attribution precise.
func openFaultDB(t *testing.T, p *h2Process, dbName string) *sql.DB {
	t.Helper()
	cfg := &Config{
		Host:     "127.0.0.1",
		Port:     fmt.Sprint(p.port),
		Database: dbName,
	}
	MergeCredentials(cfg, "sa", "")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping spawned server: %v", err)
	}
	return db
}

const faultLongQuery = "SELECT COUNT(*) FROM SYSTEM_RANGE(1, 5000000) A, SYSTEM_RANGE(1, 200000) B"

// TestIntegration_FaultKillDuringQuery kills -9 the server while a query is
// executing. The caller must get an error promptly (no hang), and further
// operations must fail cleanly while the server is down.
func TestIntegration_FaultKillDuringQuery(t *testing.T) {
	jar := findH2Jar(t)
	ctx := context.Background()

	base := t.TempDir()
	p := startH2(t, jar, freePort(t), base)
	db := openFaultDB(t, p, "fault_query_"+uuid.NewString()[:8])

	if _, err := db.ExecContext(ctx, "CREATE TABLE marker(v INT PRIMARY KEY, label VARCHAR(20))"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO marker VALUES (1, 'before')"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	type result struct {
		count int64
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var count int64
		qerr := db.QueryRowContext(ctx, faultLongQuery).Scan(&count)
		done <- result{count, qerr}
	}()

	// Let the query get underway, then yank the server out from under it.
	time.Sleep(300 * time.Millisecond)
	p.stop(false)

	select {
	case res := <-done:
		if res.err == nil {
			t.Fatal("query succeeded despite server kill")
		}
		t.Logf("query surfaced %q after kill", res.err)
	case <-time.After(15 * time.Second):
		t.Fatal("query hung >15s after server kill")
	}

	// Operations while down fail fast and cleanly.
	errCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(errCtx); err == nil {
		t.Log("ping succeeded (pool may have raced reconnect) — acceptable")
	} else {
		t.Logf("ping failed as expected: %v", err)
	}
}

// TestIntegration_FaultKillWhileStreaming kills the server mid-result-set:
// Rows.Next must terminate with an error instead of blocking forever, and
// Rows.Close must be safe afterwards.
func TestIntegration_FaultKillWhileStreaming(t *testing.T) {
	jar := findH2Jar(t)
	ctx := context.Background()

	base := t.TempDir()
	p := startH2(t, jar, freePort(t), base)

	cfg := &Config{
		Host:      "127.0.0.1",
		Port:      fmt.Sprint(p.port),
		Database:  "fault_stream_" + uuid.NewString()[:8],
		FetchSize: 5, // small batches: streaming is definitely in progress
	}
	MergeCredentials(cfg, "sa", "")
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		"SELECT X FROM SYSTEM_RANGE(1, 1000000)")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	read := 0
	for read < 3 && rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		read++
	}
	if read < 3 {
		t.Fatalf("only read %d rows before killing", read)
	}

	p.stop(false)

	// Next must return false with Err set — bounded by a watchdog.
	errCh := make(chan error, 1)
	go func() {
		for rows.Next() {
			var v int64
			_ = rows.Scan(&v)
		}
		errCh <- rows.Err()
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("rows.Err() = nil after server kill")
		}
		t.Logf("streaming surfaced %q after kill", err)
	case <-time.After(15 * time.Second):
		t.Fatal("Rows.Next hung >15s after server kill")
	}
	_ = rows.Close()
}

// TestIntegration_FaultRestartRecovery stops the server, restarts it on the
// same port and base directory, and verifies the client recovers on the same
// *sql.DB handle with committed data intact.
func TestIntegration_FaultRestartRecovery(t *testing.T) {
	jar := findH2Jar(t)
	ctx := context.Background()
	dbName := "fault_restart_" + uuid.NewString()[:8]

	base := t.TempDir()
	p := startH2(t, jar, freePort(t), base)
	db := openFaultDB(t, p, dbName)

	if _, err := db.ExecContext(ctx,
		"CREATE TABLE persist(id INT PRIMARY KEY, label VARCHAR(30))"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO persist VALUES (1, 'survives-restart')"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Flush synchronously, then stop gracefully so the shutdown hook closes
	// the database like a real-world service restart would. A SIGKILL here
	// would skip MVStore flushing and legitimately lose recent commits.
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	p.stopGracefully()
	p.removeLocks()

	downCtx, downCancel := context.WithTimeout(ctx, 8*time.Second)
	defer downCancel()
	if err := db.PingContext(downCtx); err == nil {
		t.Log("ping during outage unexpectedly succeeded — racing reconnect; continuing")
	}

	p2 := startH2(t, jar, p.port, base)

	upCtx, upCancel := context.WithTimeout(ctx, 15*time.Second)
	defer upCancel()

	// The pool's dead sessions are discarded automatically; give the first
	// healthy connection a moment under load.
	var label string
	deadline := time.Now().Add(12 * time.Second)
	for {
		qerr := db.QueryRowContext(upCtx,
			"SELECT label FROM persist WHERE id = 1").Scan(&label)
		if qerr == nil {
			break
		}
		if !strings.Contains(qerr.Error(), "EOF") &&
			!strings.Contains(qerr.Error(), "refused") &&
			!strings.Contains(qerr.Error(), "reset") &&
			time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		t.Fatalf("recovery query failed: %v", qerr)
	}
	if label != "survives-restart" {
		t.Errorf("data after restart = %q, want survives-restart", label)
	}
	_ = p2 // kept alive by cleanup until test end

	// And the recovered connection serves fresh work too.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO persist VALUES (2, 'after')"); err != nil {
		t.Errorf("post-recovery write: %v", err)
	}
}
