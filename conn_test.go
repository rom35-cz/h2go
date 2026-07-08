package h2go

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

// TestConnImplementsInterfaces verifies conn implements required interfaces.
func TestConnImplementsInterfaces(_ *testing.T) {
	var _ driver.Conn = (*conn)(nil)
	var _ driver.ConnBeginTx = (*conn)(nil)
	var _ driver.ConnPrepareContext = (*conn)(nil)
	var _ driver.Pinger = (*conn)(nil)
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
// when the connection is closed.
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
// is already in use (busy).
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
	// Should NOT be ErrBadConn (that's for closed connections).
	if err == driver.ErrBadConn {
		t.Error("expected non-ErrBadConn error for busy connection, got ErrBadConn")
	}
}
