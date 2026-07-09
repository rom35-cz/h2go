package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"time"
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
// If a transaction is open but idle (no wire operation currently in flight),
// Close sends a best-effort ROLLBACK before ending the session so the H2
// server releases locks immediately rather than waiting for the TCP teardown.
// When a commit/rollback is already in flight (c.busy == true), the extra
// rollback is skipped — the in-flight operation will naturally fail once
// sess.Close() shuts the transport, and its error path will inform the caller.
//
// Close implements driver.Conn.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess == nil {
		return nil
	}

	// If an operation is currently in flight the caller must not close the
	// connection concurrently; doing so would race on Session.tr.
	// database/sql guarantees Close is never called while a connection is
	// busy. This guard is purely defensive: abandon session state without
	// touching the transport so the in-flight goroutine keeps a clean error.
	if c.busy {
		c.sess = nil
		c.busy = false
		return nil
	}

	// Roll back any open, idle transaction. Errors are ignored: the session
	// is being destroyed regardless and H2 will roll back on TCP close anyway.
	if !c.sess.autoCommit {
		_ = c.sess.rollbackCurrentTransaction(context.Background())
	}

	err := c.sess.Close()
	c.sess = nil
	c.busy = false
	return err
}

// Prepare prepares a statement for later queries or executions.
//
// Prepare implements driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// Begin starts a new transaction.
//
// Begin implements driver.Conn.
func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx begins a new transaction with the provided context and
// transaction options.
//
// BeginTx implements driver.ConnBeginTx.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return beginTx(ctx, c, opts)
}

// PrepareContext prepares a statement with the provided context.
//
// PrepareContext implements driver.ConnPrepareContext.
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.acquire(); err != nil {
		return nil, err
	}
	defer c.release()

	cmd, err := c.sess.PrepareCommandReadParams(ctx, query)
	if err != nil {
		return nil, err
	}

	return &stmt{conn: c, cmd: cmd}, nil
}

// Ping validates the connection by executing SELECT 1 and draining
// the result. This confirms the session is alive and can execute queries.
//
// Ping implements driver.Pinger.
func (c *conn) Ping(ctx context.Context) error {
	// Execute SELECT 1 as a lightweight probe. Preserve the original error from
	// query execution; only truly closed connections already surface as
	// driver.ErrBadConn from acquire().
	rows, err := c.queryContextInternal(ctx, "SELECT 1")
	if err != nil {
		return err
	}

	dest := make([]driver.Value, len(rows.Columns()))
	for {
		err := rows.Next(dest)
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = rows.Close()
			return err
		}
	}
	return rows.Close()
}

// IsValid asks H2 whether the session is still alive by round-tripping the
// SESSION_HAS_PENDING_TRANSACTION probe. The result itself is ignored; any
// transport or session error means the connection should not be reused.
//
// IsValid implements driver.Validator.
func (c *conn) IsValid() bool {
	if c == nil {
		return false
	}
	if err := c.acquire(); err != nil {
		return false
	}
	defer c.release()

	if c.sess == nil {
		return false
	}
	_, err := c.sess.hasPendingTransaction(context.Background())
	return err == nil
}

// ResetSession rolls back any pending transaction, restores autocommit, and
// clears the connection state before the pool reuses the session.
// Broken sessions return driver.ErrBadConn so database/sql discards them.
//
// ResetSession implements driver.SessionResetter.
func (c *conn) ResetSession(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.sess == nil {
		return driver.ErrBadConn
	}
	if err := c.acquire(); err != nil {
		return driver.ErrBadConn
	}
	defer c.release()

	pending, err := c.sess.hasPendingTransaction(ctx)
	if err != nil {
		return driver.ErrBadConn
	}
	if pending {
		if err := c.sess.rollbackCurrentTransaction(ctx); err != nil {
			return driver.ErrBadConn
		}
	}
	if !c.sess.autoCommit {
		if err := c.sess.setAutoCommit(ctx, true); err != nil {
			return driver.ErrBadConn
		}
	}
	return nil
}

// acquire marks the connection as busy, preventing concurrent use.
// Returns an error if the connection is already busy or closed.
// Callers must defer release() after a successful acquire.
func (c *conn) acquire() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess == nil || c.sess.dead.Load() {
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

// QueryContext executes a query that may return rows, such as a SELECT.
// Parameterless queries are executed directly; queries with positional ? args
// are encoded inline using the T6.1 value encoder (consistent with ExecContext).
//
// QueryContext implements driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) == 0 {
		return c.queryContextInternal(ctx, query)
	}

	params, err := convertNamedValues(args)
	if err != nil {
		return nil, err
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	// release happens in rows.Close() via closeCallback

	rows, err := c.sess.ExecuteQueryWithParams(ctx, query, 0, defaultFetchSize, params)
	if err != nil {
		c.release()
		return nil, err
	}
	rows.closeCallback = func() error {
		c.release()
		return nil
	}
	return rows, nil
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
	rows.closeCallback = func() error {
		c.release()
		return nil
	}

	return rows, nil
}

// ExecContext executes a query that doesn't return rows, such as
// an INSERT, UPDATE, DELETE, or DDL statement.
// For MVP, this only supports parameterless queries.
//
// ExecContext implements driver.ExecerContext.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	var (
		params []driver.Value
		err    error
	)
	if len(args) > 0 {
		params, err = convertNamedValues(args)
		if err != nil {
			return nil, err
		}
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	defer c.release()

	var res *ResultWithUpdateCount
	if len(params) == 0 {
		res, err = c.sess.ExecuteUpdate(ctx, query)
	} else {
		res, err = c.sess.ExecuteUpdateWithParams(ctx, query, params)
	}
	if err != nil {
		return nil, err
	}

	return &result{affected: res.UpdateCount}, nil
}

// CheckNamedValue validates and normalizes one argument for this connection.
//
// It enforces positional-only placeholders, converts driver.Valuer values
// (including github.com/google/uuid.UUID via Value()), and restricts values to
// the MVP supported set.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	return normalizeNamedValue(nv)
}

// normalizeNamedValue validates and converts one argument in-place.
func normalizeNamedValue(nv *driver.NamedValue) error {
	if nv.Name != "" {
		return fmt.Errorf("h2go: named parameters are not supported; use positional ? placeholders")
	}
	if nv.Ordinal < 1 {
		return fmt.Errorf("h2go: invalid parameter ordinal %d", nv.Ordinal)
	}

	v, err := driver.DefaultParameterConverter.ConvertValue(nv.Value)
	if err != nil {
		return fmt.Errorf("h2go: parameter %d conversion failed: %w", nv.Ordinal, err)
	}

	switch v.(type) {
	case nil, bool, int64, float64, string, []byte, time.Time:
		nv.Value = v
		return nil
	default:
		return fmt.Errorf("h2go: parameter %d has unsupported type %T", nv.Ordinal, v)
	}
}

// convertNamedValues converts arguments to supported driver.Value types for wire
// encoding and verifies ordinal order is contiguous and positional.
func convertNamedValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i := range args {
		arg := args[i]
		if err := normalizeNamedValue(&arg); err != nil {
			return nil, err
		}
		if arg.Ordinal != i+1 {
			return nil, fmt.Errorf("h2go: invalid parameter ordinal %d at index %d", arg.Ordinal, i)
		}
		values[i] = arg.Value
	}
	return values, nil
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
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
)
