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

	mu     sync.Mutex
	closed bool
}

// Close releases the server-side prepared command.
func (s *stmt) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	c := s.conn
	cmd := s.cmd
	s.conn = nil
	s.cmd = nil
	s.mu.Unlock()

	if c == nil || cmd == nil {
		return nil
	}

	if err := c.acquire(); err != nil {
		return err
	}
	defer c.release()

	return cmd.Close(c.sess)
}

// NumInput returns the number of placeholder parameters.
func (s *stmt) NumInput() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil {
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
	c, cmd, err := s.snapshotOpen()
	if err != nil {
		return nil, err
	}

	params, err := convertNamedValues(args)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	defer c.release()

	res, err := c.sess.ExecuteUpdatePreparedWithParams(cmd, params)
	if err != nil {
		return nil, err
	}
	return &result{affected: res.UpdateCount}, nil
}

// QueryContext executes the prepared statement as a query.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	c, cmd, err := s.snapshotOpen()
	if err != nil {
		return nil, err
	}

	params, err := convertNamedValues(args)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	// For query rows, release happens on rows.Close().

	rows, err := c.sess.ExecuteQueryPreparedWithParams(ctx, cmd, 0, defaultFetchSize, params)
	if err != nil {
		c.release()
		return nil, err
	}

	rows.closeCallback = func() {
		c.release()
	}
	return rows, nil
}

// CheckNamedValue validates and normalizes one argument for this statement.
func (s *stmt) CheckNamedValue(nv *driver.NamedValue) error {
	return normalizeNamedValue(nv)
}

func (s *stmt) snapshotOpen() (*conn, *PreparedCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.conn == nil || s.cmd == nil {
		return nil, nil, fmt.Errorf("h2go: statement is closed")
	}
	return s.conn, s.cmd, nil
}

var (
	_ driver.Stmt              = (*stmt)(nil)
	_ driver.StmtExecContext   = (*stmt)(nil)
	_ driver.StmtQueryContext  = (*stmt)(nil)
	_ driver.NamedValueChecker = (*stmt)(nil)
)
