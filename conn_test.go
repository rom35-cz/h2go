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
}

// TestConnPrepareNotSupported verifies Prepare returns not-yet-supported error.
func TestConnPrepareNotSupported(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.Prepare("SELECT 1")
	if err == nil {
		t.Fatal("expected error for not-yet-supported Prepare")
	}
	if !errors.Is(err, ErrNotYetSupported) {
		t.Errorf("expected ErrNotYetSupported, got %v", err)
	}
}

// TestConnBeginNotSupported verifies Begin returns not-yet-supported error.
func TestConnBeginNotSupported(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.Begin()
	if err == nil {
		t.Fatal("expected error for not-yet-supported Begin")
	}
	if !errors.Is(err, ErrNotYetSupported) {
		t.Errorf("expected ErrNotYetSupported, got %v", err)
	}
}

// TestConnBeginTxNotSupported verifies BeginTx returns not-yet-supported error.
func TestConnBeginTxNotSupported(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.BeginTx(context.Background(), driver.TxOptions{})
	if err == nil {
		t.Fatal("expected error for not-yet-supported BeginTx")
	}
	if !errors.Is(err, ErrNotYetSupported) {
		t.Errorf("expected ErrNotYetSupported, got %v", err)
	}
}

// TestConnPrepareContextNotSupported verifies PrepareContext returns error.
func TestConnPrepareContextNotSupported(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.PrepareContext(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("expected error for not-yet-supported PrepareContext")
	}
	if !errors.Is(err, ErrNotYetSupported) {
		t.Errorf("expected ErrNotYetSupported, got %v", err)
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

// TestConnPingBusy verifies Ping returns an error when the connection
// is already in use (busy). Ping now executes SELECT 1 which requires
// acquiring the connection first.
func TestConnPingBusy(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	// Acquire the connection (simulating an in-progress operation).
	err := c.acquire()
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Ping should fail because the connection is busy.
	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for Ping while busy")
	}
	// Ping on busy connection now returns ErrBadConn because the SELECT 1
	// cannot execute without acquiring the lock.
	if err != driver.ErrBadConn {
		t.Errorf("expected ErrBadConn for busy connection with SELECT 1 ping, got %v", err)
	}
}

// TestConnQueryContextWithArgs verifies QueryContext returns ErrSkip
// when arguments are provided (parameter support not yet implemented).
func TestConnQueryContextWithArgs(t *testing.T) {
	c := &conn{
		sess: &Session{id: "test-session"},
	}

	_, err := c.QueryContext(context.Background(), "SELECT * FROM t WHERE id = ?", []driver.NamedValue{
		{Name: "", Ordinal: 1, Value: int64(1)},
	})
	if err != driver.ErrSkip {
		t.Errorf("expected ErrSkip for parameterized query, got %v", err)
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
				{Name: "", Ordinal: 2, Value: "hello"},
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
