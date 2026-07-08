package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
)

// ErrNotYetSupported is returned for operations that are not yet
// implemented in the current MVP phase. Users should check the
// implementation plan for when these features will be available.
var ErrNotYetSupported = fmt.Errorf("h2go: operation not yet supported")

// conn implements driver.Conn, representing a single connection
// to an H2 database server.
//
// Each conn holds an authenticated Session and guards against
// concurrent use since the H2 TCP protocol is request-response
// per connection (a connection cannot interleave operations).
type conn struct {
	sess *Session
	mu   sync.Mutex
	busy bool
}

// Close closes the connection, sending SESSION_CLOSE to the server
// and releasing the underlying TCP connection.
//
// Close implements driver.Conn.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess == nil {
		return nil
	}

	err := c.sess.Close()
	c.sess = nil
	c.busy = false
	return err
}

// Prepare returns a prepared statement for the given query.
//
// Note: This is a placeholder implementation that returns an error
// indicating prepared statements are not yet supported. Full
// prepared statement support will be implemented in Phase 6 (T6.3).
//
// Prepare implements driver.Conn.
func (c *conn) Prepare(_ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("h2go: Prepare: %w (prepared statements coming in Phase 6)", ErrNotYetSupported)
}

// Begin starts a new transaction.
//
// Note: This is a placeholder implementation that returns an error
// indicating transactions are not yet supported. Full transaction
// support will be implemented in Phase 8 (T8.1). Until then,
// operations run in autocommit mode as returned by the server
// handshake.
//
// Begin implements driver.Conn.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("h2go: Begin: %w (transactions coming in Phase 8)", ErrNotYetSupported)
}

// BeginTx begins a new transaction with the provided context and
// transaction options.
//
// Note: This is a placeholder implementation that returns an error
// indicating transactions are not yet supported. Full transaction
// support will be implemented in Phase 8 (T8.1).
//
// BeginTx implements driver.ConnBeginTx.
func (c *conn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("h2go: BeginTx: %w (transactions coming in Phase 8)", ErrNotYetSupported)
}

// PrepareContext prepares a statement with the provided context.
//
// Note: This is a placeholder implementation that returns an error
// indicating prepared statements are not yet supported. Full
// prepared statement support will be implemented in Phase 6 (T6.3).
//
// PrepareContext implements driver.ConnPrepareContext.
func (c *conn) PrepareContext(_ context.Context, _ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("h2go: PrepareContext: %w (prepared statements coming in Phase 6)", ErrNotYetSupported)
}

// Ping validates the connection by performing a lightweight round-trip
// to the server using SESSION_HAS_PENDING_TRANSACTION. This is an interim
// implementation; in Phase 5 (T5.3) it will be re-pointed to execute
// "SELECT 1" once query execution is available.
//
// Ping implements driver.Pinger.
func (c *conn) Ping(_ context.Context) error {
	if err := c.acquire(); err != nil {
		return err
	}
	defer c.release()

	// Send SESSION_HAS_PENDING_TRANSACTION as a lightweight probe.
	// This operation returns a boolean (true if there are uncommitted changes).
	// We don't care about the value, only that the server responds.
	if err := c.sess.tr.WriteInt32(SessionHasPendingTransaction); err != nil {
		return driver.ErrBadConn
	}
	if err := c.sess.tr.Flush(); err != nil {
		return driver.ErrBadConn
	}

	// Read status.
	status, err := c.sess.tr.ReadInt32()
	if err != nil {
		return driver.ErrBadConn
	}
	if status != StatusOK {
		// Server returned an error or unexpected status.
		// Read and discard the error details for now.
		if status == StatusError {
			_ = readH2Error(c.sess.tr)
		}
		return driver.ErrBadConn
	}

	// Read the boolean result (has pending transaction).
	_, err = c.sess.tr.ReadBool()
	if err != nil {
		return driver.ErrBadConn
	}

	return nil
}

// acquire marks the connection as busy, preventing concurrent use.
// Returns an error if the connection is already busy or closed.
// Callers must defer release() after a successful acquire.
func (c *conn) acquire() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess == nil {
		return driver.ErrBadConn
	}
	if c.busy {
		return fmt.Errorf("h2go: connection already in use (concurrent operations not supported)")
	}
	c.busy = true
	return nil
}

// release marks the connection as no longer busy.
func (c *conn) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
}

// Verify interface compliance at compile time.
var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	// Validator and SessionResetter in T9.1
	// QueryerContext and ExecerContext in Phase 5/6
)
