package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
)

// stmt implements driver.Stmt and context-aware statement interfaces for a
// server-side prepared command.
type stmt struct {
	conn *conn
	cmd  *PreparedCommand

	mu           sync.Mutex
	closed       bool // Close requested or completed; blocks new executions.
	closePending bool // Close was requested while this statement's conn was busy.
	closeSent    bool // COMMAND_CLOSE was successfully sent or no command remains.
}

// Close releases the server-side prepared command.
//
// If Close races with an in-flight ExecContext/QueryContext, new executions are
// rejected immediately and the actual COMMAND_CLOSE is deferred until the
// in-flight operation (or its rows) releases the connection. This avoids both a
// use-after-close of the server command and the command leak caused by dropping
// the client-side command ID when the connection is busy.
func (s *stmt) Close() error {
	s.mu.Lock()
	if s.closeSent || s.cmd == nil {
		s.closed = true
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	c := s.conn
	cmd := s.cmd
	s.mu.Unlock()

	if c == nil || cmd == nil {
		s.markCommandClosed(cmd)
		return nil
	}

	if err := c.acquire(); err != nil {
		// The connection is currently owned by another operation. Keep the command
		// reference so the owning operation can close it before releasing the conn.
		s.mu.Lock()
		if !s.closeSent {
			s.closePending = true
		}
		s.mu.Unlock()
		return nil
	}
	defer c.release()

	if err := cmd.Close(c.sess); err != nil {
		return err
	}
	s.markCommandClosed(cmd)
	return nil
}

// NumInput returns the number of placeholder parameters.
func (s *stmt) NumInput() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cmd == nil {
		return -1
	}
	return int(s.cmd.ParamCount)
}

// Exec executes the statement with positional parameters.
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.ExecContext(context.Background(), nv)
}

// Query executes the statement with positional parameters and returns rows.
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.QueryContext(context.Background(), nv)
}

// ExecContext executes the prepared statement as an update.
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	params, err := convertNamedValues(args)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	c, cmd, err := s.beginOperation()
	if err != nil {
		return nil, err
	}

	res, execErr := c.sess.ExecuteUpdatePreparedWithParams(cmd, params)
	closeErr := s.finishOperation(c)
	if execErr != nil {
		return nil, execErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return &result{affected: res.UpdateCount}, nil
}

// QueryContext executes the prepared statement as a query.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	params, err := convertNamedValues(args)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	c, cmd, err := s.beginOperation()
	if err != nil {
		return nil, err
	}
	// For query rows, release happens on rows.Close().

	rows, err := c.sess.ExecuteQueryPreparedWithParams(ctx, cmd, 0, defaultFetchSize, params)
	if err != nil {
		closeErr := s.finishOperation(c)
		if closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}

	rows.closeCallback = func() error {
		return s.finishOperation(c)
	}
	return rows, nil
}

// CheckNamedValue validates and normalizes one argument for this statement.
func (s *stmt) CheckNamedValue(nv *driver.NamedValue) error {
	return normalizeNamedValue(nv)
}

func (s *stmt) beginOperation() (*conn, *PreparedCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.conn == nil || s.cmd == nil || s.closeSent {
		return nil, nil, fmt.Errorf("h2go: statement is closed")
	}
	c := s.conn
	cmd := s.cmd
	if err := c.acquire(); err != nil {
		return nil, nil, err
	}
	return c, cmd, nil
}

// finishOperation runs while c is still acquired by this statement operation.
// It performs a deferred COMMAND_CLOSE when Close was requested during the
// operation, then releases the connection.
func (s *stmt) finishOperation(c *conn) error {
	defer c.release()
	return s.closePendingOnOwnedConn(c)
}

func (s *stmt) closePendingOnOwnedConn(c *conn) error {
	s.mu.Lock()
	if !s.closed || !s.closePending || s.closeSent || s.cmd == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := s.cmd
	s.mu.Unlock()

	if err := cmd.Close(c.sess); err != nil {
		return err
	}
	s.markCommandClosed(cmd)
	return nil
}

func (s *stmt) markCommandClosed(cmd *PreparedCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cmd == nil || s.cmd == cmd {
		s.cmd = nil
		s.conn = nil
		s.closePending = false
		s.closeSent = true
	}
}

var (
	_ driver.Stmt              = (*stmt)(nil)
	_ driver.StmtExecContext   = (*stmt)(nil)
	_ driver.StmtQueryContext  = (*stmt)(nil)
	_ driver.NamedValueChecker = (*stmt)(nil)
)
