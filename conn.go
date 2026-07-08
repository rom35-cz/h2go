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

// Ping validates the connection by executing SELECT 1 and draining
// the result. This confirms the session is alive and can execute queries.
//
// Ping implements driver.Pinger.
func (c *conn) Ping(ctx context.Context) error {
	// Execute SELECT 1 as a lightweight probe
	rows, err := c.queryContextInternal(ctx, "SELECT 1")
	if err != nil {
		return driver.ErrBadConn
	}
	// Drain and close the result
	_ = rows.Close()
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

// QueryContext executes a query that may return rows, such as SELECT.
// For MVP, this only supports parameterless queries. If args are provided
// before Phase 6 parameter support is complete, it returns ErrSkip so
// database/sql can fall back to Prepare + Query.
//
// QueryContext implements driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		// Parameters not yet supported in T5.3; let database/sql fall back
		return nil, driver.ErrSkip
	}
	return c.queryContextInternal(ctx, query)
}

// queryContextInternal is the internal implementation for executing a query.
// It handles the acquire/release lifecycle but assumes args are already checked.
func (c *conn) queryContextInternal(ctx context.Context, query string) (driver.Rows, error) {
	if err := c.acquire(); err != nil {
		return nil, err
	}
	// Note: release happens in rows.Close() after the result is consumed,
	// since the connection must remain busy while rows are being read.

	rows, err := c.sess.ExecuteQuery(ctx, query, 0, defaultFetchSize)
	if err != nil {
		c.release()
		return nil, err
	}

	// Set up cleanup callback for when rows are closed
	rows.closeCallback = func() {
		c.release()
	}

	return rows, nil
}

// ExecContext executes a query that doesn't return rows, such as
// an INSERT, UPDATE, DELETE, or DDL statement.
// For MVP, this only supports parameterless queries.
//
// ExecContext implements driver.ExecerContext.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) > 0 {
		// Parameters not yet supported; let database/sql fall back
		return nil, driver.ErrSkip
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	defer c.release()

	res, err := c.sess.ExecuteUpdate(ctx, query)
	if err != nil {
		return nil, err
	}

	return &result{affected: res.UpdateCount}, nil
}

// defaultFetchSize is the default number of rows to fetch in one batch.
// This matches H2's default fetch size behavior.
const defaultFetchSize = 100

// Verify interface compliance at compile time.
var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	// Validator and SessionResetter in T9.1
)
