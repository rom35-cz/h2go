package h2go

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestConnImplementsInterfaces verifies conn implements required interfaces.
func TestConnImplementsInterfaces(_ *testing.T) {
	var _ driver.Conn = (*conn)(nil)
	var _ driver.ConnBeginTx = (*conn)(nil)
	var _ driver.ConnPrepareContext = (*conn)(nil)
	var _ driver.Pinger = (*conn)(nil)
	var _ driver.QueryerContext = (*conn)(nil)
	var _ driver.ExecerContext = (*conn)(nil)
	var _ driver.NamedValueChecker = (*conn)(nil)
}

// TestConnPrepareSessionClosed verifies Prepare reports a session/transport error
// when called on a mock session without wire transport.
func TestConnPrepareSessionClosed(t *testing.T) {
	c := &conn{sess: &Session{id: "test-session"}}

	_, err := c.Prepare("SELECT 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrNotYetSupported) {
		t.Errorf("Prepare should be implemented; got ErrNotYetSupported")
	}
	if !strings.Contains(err.Error(), "session closed") {
		t.Errorf("expected session closed error, got %v", err)
	}
}

// TestConnBeginSessionClosed verifies Begin returns a session/transport error
// when the connection has a session but no live transport.
func TestConnBeginSessionClosed(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session", autoCommit: true},
	}

	_, err := c.Begin()
	if err == nil {
		t.Fatal("expected error for Begin on a mock session")
	}
	if !strings.Contains(err.Error(), "session closed") {
		t.Errorf("expected session closed error, got %v", err)
	}
}

// TestConnBeginTxSessionClosed verifies BeginTx returns a session/transport
// error when the connection has a session but no live transport.
func TestConnBeginTxSessionClosed(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session", autoCommit: true},
	}

	_, err := c.BeginTx(context.Background(), driver.TxOptions{})
	if err == nil {
		t.Fatal("expected error for BeginTx on a mock session")
	}
	if !strings.Contains(err.Error(), "session closed") {
		t.Errorf("expected session closed error, got %v", err)
	}
}

// TestConnBeginTxRejectsActiveTx verifies nested BeginTx calls are rejected
// when the session is already in non-autocommit mode.
func TestConnBeginTxRejectsActiveTx(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session", autoCommit: false},
	}

	_, err := c.BeginTx(context.Background(), driver.TxOptions{})
	if err == nil {
		t.Fatal("expected error for BeginTx while transaction is active")
	}
	if !strings.Contains(err.Error(), "transaction already active") {
		t.Errorf("expected active transaction error, got %v", err)
	}
}

// TestConnCloseWithOpenTransaction verifies that Close issues a best-effort
// rollback when a transaction is open (autoCommit == false) but idle.
// Even with no live transport the call must complete without panicking and
// must nil out the session so subsequent operations return ErrBadConn.
func TestConnCloseWithOpenTransaction(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session", autoCommit: false},
	}

	// Close must not panic even though there is no wire transport.
	// rollbackCurrentTransaction returns an error (session closed / nil tr),
	// which Close discards, then proceeds to nil sess.
	_ = c.Close()

	if c.sess != nil {
		t.Error("Close did not nil out sess")
	}
	// Subsequent acquire must return ErrBadConn.
	if err := c.acquire(); err != driver.ErrBadConn {
		t.Errorf("expected ErrBadConn after Close, got %v", err)
	}
}

// TestConnCloseWithOpenTransactionBusy verifies that Close skips the
// best-effort rollback when a wire operation is already in flight (c.busy).
// This prevents a concurrent Close from issuing a second rollback while
// commit/rollback wire I/O is active on the transport.
func TestConnCloseWithOpenTransactionBusy(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session", autoCommit: false},
		busy: true,
	}

	// Close must not panic and must still nil sess.
	_ = c.Close()

	if c.sess != nil {
		t.Error("Close did not nil out sess")
	}
}

// TestConnBeginTxRejectsReadOnly verifies the driver rejects read-only
// transaction options with a clear error.
func TestConnBeginTxRejectsReadOnly(t *testing.T) {
	c := &conn{sess: &Session{id: "test-session", autoCommit: true}}

	_, err := c.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true})
	if err == nil {
		t.Fatal("expected error for read-only BeginTx")
	}
	if !strings.Contains(err.Error(), "read-only transactions are not supported") {
		t.Errorf("expected read-only error, got %v", err)
	}
}

// TestConnBeginTxRejectsUnsupportedIsolation verifies unsupported isolation
// levels return a clear error before any wire I/O happens.
func TestConnBeginTxRejectsUnsupportedIsolation(t *testing.T) {
	c := &conn{sess: &Session{id: "test-session", autoCommit: true}}

	_, err := c.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(999)})
	if err == nil {
		t.Fatal("expected error for unsupported isolation BeginTx")
	}
	if !strings.Contains(err.Error(), "unknown isolation level") {
		t.Errorf("expected unknown isolation error, got %v", err)
	}
}

// TestConnPrepareContextNoSession verifies PrepareContext returns ErrBadConn
// when the connection has no live session.
func TestConnPrepareContextNoSession(t *testing.T) {
	c := &conn{sess: nil}

	_, err := c.PrepareContext(context.Background(), "SELECT 1")
	if err != driver.ErrBadConn {
		t.Fatalf("expected ErrBadConn, got %v", err)
	}
}

// TestConnCloseNilSession verifies Close is safe when session is nil.
func TestConnCloseNilSession(t *testing.T) {
	c := &conn{
		sess: nil,
	}

	err := c.Close()
	if err != nil {
		t.Errorf("expected nil error for closing nil session, got %v", err)
	}
}

// TestConnAcquireRelease verifies the busy flag guards concurrent use.
func TestConnAcquireRelease(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	// First acquire should succeed.
	err := c.acquire()
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should fail (already busy).
	err = c.acquire()
	if err == nil {
		t.Fatal("expected error for second acquire while busy")
	}

	// After release, acquire should succeed again.
	c.release()

	err = c.acquire()
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
}

// TestConnAcquireClosedConnection verifies acquire returns ErrBadConn
// when the connection is closed.
func TestConnAcquireClosedConnection(t *testing.T) {
	c := &conn{
		sess: nil, // simulates closed connection
	}

	err := c.acquire()
	if err == nil {
		t.Fatal("expected error for acquire on closed connection")
	}
	if err != driver.ErrBadConn {
		t.Errorf("expected driver.ErrBadConn, got %v", err)
	}
}

// TestConnAcquireAfterClose verifies acquire returns ErrBadConn after Close.
func TestConnAcquireAfterClose(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test"},
	}

	// Initially can acquire.
	err := c.acquire()
	if err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}
	c.release()

	// Close sets sess to nil.
	c.sess = nil

	// Now acquire should fail.
	err = c.acquire()
	if err != driver.ErrBadConn {
		t.Errorf("expected ErrBadConn after close, got %v", err)
	}
}

// TestErrNotYetSupported is a sentinel error that can be checked.
func TestErrNotYetSupported(t *testing.T) {
	if ErrNotYetSupported == nil {
		t.Fatal("ErrNotYetSupported should not be nil")
	}
	if ErrNotYetSupported.Error() == "" {
		t.Fatal("ErrNotYetSupported should have a message")
	}
}

// TestConnPingClosedConnection verifies Ping returns ErrBadConn
// when the connection is closed (because it tries to execute SELECT 1).
func TestConnPingClosedConnection(t *testing.T) {
	c := &conn{
		sess: nil, // closed
	}

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for Ping on closed connection")
	}
	if err != driver.ErrBadConn {
		t.Errorf("expected driver.ErrBadConn, got %v", err)
	}
}

// TestConnPingBusy verifies Ping preserves the busy/concurrent-use error
// instead of misclassifying a healthy-but-busy connection as driver.ErrBadConn.
func TestConnPingBusy(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	// Acquire the connection (simulating an in-progress operation).
	err := c.acquire()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer c.release()

	// Ping should fail because the connection is busy, but this is not a broken
	// socket/session and should not poison the pool as ErrBadConn.
	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for Ping while busy")
	}
	if err == driver.ErrBadConn {
		t.Errorf("busy Ping must not return ErrBadConn")
	}
	if !strings.Contains(err.Error(), "connection already in use") {
		t.Errorf("expected busy error, got %v", err)
	}
}

// TestConnIsValidClosedConnection verifies IsValid returns false when the
// session is already closed.
func TestConnIsValidClosedConnection(t *testing.T) {
	c := &conn{sess: nil}
	if c.IsValid() {
		t.Fatal("expected IsValid to report false for closed connection")
	}
}

// TestConnResetSessionClosedConnection verifies ResetSession returns
// driver.ErrBadConn when the connection has already been closed.
func TestConnResetSessionClosedConnection(t *testing.T) {
	c := &conn{sess: nil}
	if err := c.ResetSession(context.Background()); err != driver.ErrBadConn {
		t.Fatalf("expected ErrBadConn for closed ResetSession, got %v", err)
	}
}

// TestConnQueryContextWithArgs verifies QueryContext takes the parameterized
// execution path inline (no ErrSkip). The mock session has no wire transport,
// so the request fails with a transport error rather than ErrSkip.
func TestConnQueryContextWithArgs(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.QueryContext(context.Background(), "SELECT * FROM t WHERE id = ?", []driver.NamedValue{
		{Name: "", Ordinal: 1, Value: int64(1)},
	})
	if err == nil {
		t.Fatal("expected execution error for mock session")
	}
	if errors.Is(err, driver.ErrSkip) {
		t.Errorf("QueryContext with args must not return ErrSkip; got ErrSkip")
	}
}

// TestConnExecContextWithArgs verifies ExecContext takes the parameterized
// execution path (no longer ErrSkip). The mock session has no wire transport,
// so the request fails later with a regular execution error.
func TestConnExecContextWithArgs(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.ExecContext(context.Background(), "INSERT INTO t VALUES (?)", []driver.NamedValue{
		{Name: "", Ordinal: 1, Value: int64(1)},
	})
	if err == nil {
		t.Fatal("expected execution error for mock session")
	}
	if errors.Is(err, driver.ErrSkip) {
		t.Errorf("expected parameterized exec path, got ErrSkip")
	}
}

type testValuerString string

func (v testValuerString) Value() (driver.Value, error) {
	return string(v), nil
}

func TestConnCheckNamedValue(t *testing.T) {
	c := &conn{}

	nv := driver.NamedValue{Ordinal: 1, Value: testValuerString("abc")}
	if err := c.CheckNamedValue(&nv); err != nil {
		t.Fatalf("CheckNamedValue failed: %v", err)
	}
	if got, ok := nv.Value.(string); !ok || got != "abc" {
		t.Fatalf("converted value: got %T(%v), want string(abc)", nv.Value, nv.Value)
	}
}

func TestConvertNamedValues(t *testing.T) {
	tests := []struct {
		name    string
		in      []driver.NamedValue
		wantErr string
	}{
		{
			name: "positional ok",
			in: []driver.NamedValue{
				{Name: "", Ordinal: 1, Value: 7},
				{Name: "", Ordinal: 2, Value: testValuerString("hello")},
				{Name: "", Ordinal: 3, Value: []byte{0xAB}},
				{Name: "", Ordinal: 4, Value: time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC)},
			},
		},
		{
			name: "named rejected",
			in: []driver.NamedValue{
				{Name: "id", Ordinal: 1, Value: 1},
			},
			wantErr: "named parameters are not supported",
		},
		{
			name: "ordinal mismatch",
			in: []driver.NamedValue{
				{Name: "", Ordinal: 2, Value: 1},
			},
			wantErr: "invalid parameter ordinal",
		},
		{
			name: "unsupported",
			in: []driver.NamedValue{
				{Name: "", Ordinal: 1, Value: complex(1, 2)},
			},
			wantErr: "conversion failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vals, err := convertNamedValues(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertNamedValues failed: %v", err)
			}
			if len(vals) != len(tc.in) {
				t.Fatalf("len(vals)=%d, want %d", len(vals), len(tc.in))
			}
			if _, ok := vals[0].(int64); !ok {
				t.Fatalf("vals[0] type=%T, want int64", vals[0])
			}
			if got, ok := vals[1].(string); !ok || got != "hello" {
				t.Fatalf("vals[1]=%T(%v), want string(hello)", vals[1], vals[1])
			}
		})
	}
}

// TestResultRowsAffected verifies result.RowsAffected returns the stored value.
func TestResultRowsAffected(t *testing.T) {
	r := &result{affected: 42}

	affected, err := r.RowsAffected()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if affected != 42 {
		t.Errorf("expected 42, got %d", affected)
	}
}

// TestResultLastInsertId verifies result.LastInsertId returns an error
// (not supported in MVP).
func TestResultLastInsertId(t *testing.T) {
	r := &result{affected: 0}

	id, err := r.LastInsertId()
	if err == nil {
		t.Error("expected error for LastInsertId in MVP")
	}
	if id != 0 {
		t.Errorf("expected 0, got %d", id)
	}
}
