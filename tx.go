package h2go

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
)

const defaultTxIsolationSQL = "READ COMMITTED"

// tx implements driver.Tx for a single H2 transaction.
type tx struct {
	c *conn

	// restoreIsolation indicates that BeginTx changed the session isolation
	// level and Commit/Rollback should restore the default H2 isolation level
	// after the transaction finishes.
	restoreIsolation bool

	mu   sync.Mutex
	done bool
}

// beginTx starts a transaction on c using the provided context and options.
// It is invoked by conn.Begin and conn.BeginTx.
func beginTx(ctx context.Context, c *conn, opts driver.TxOptions) (driver.Tx, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.sess == nil {
		return nil, driver.ErrBadConn
	}

	if opts.ReadOnly {
		return nil, fmt.Errorf("h2go: BeginTx: read-only transactions are not supported")
	}
	isolationSQL, restoreIsolation, err := txIsolationSQL(opts.Isolation)
	if err != nil {
		return nil, err
	}

	if err := c.acquire(); err != nil {
		return nil, err
	}
	released := false
	defer func() {
		if !released {
			c.release()
		}
	}()

	if !c.sess.autoCommit {
		return nil, fmt.Errorf("h2go: BeginTx: transaction already active")
	}

	if err := c.sess.setAutoCommit(ctx, false); err != nil {
		return nil, err
	}

	if isolationSQL != "" {
		if err := c.sess.setTransactionIsolation(ctx, isolationSQL); err != nil {
			// Best-effort rollback to the original autocommit state. If this fails,
			// surface both errors so the caller knows the connection is suspect.
			if restoreErr := c.sess.setAutoCommit(context.Background(), true); restoreErr != nil {
				err = errors.Join(err, restoreErr)
			}
			return nil, err
		}
	}

	released = true
	c.release()
	return &tx{c: c, restoreIsolation: restoreIsolation}, nil
}

// Commit commits the transaction and restores autocommit on success.
func (t *tx) Commit() error {
	return t.finishTransaction(false)
}

// Rollback rolls back the transaction and restores autocommit on success.
func (t *tx) Rollback() error {
	return t.finishTransaction(true)
}

func (t *tx) finishTransaction(rollback bool) error {
	if t == nil {
		return sql.ErrTxDone
	}

	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return sql.ErrTxDone
	}
	t.mu.Unlock()

	if t.c == nil || t.c.sess == nil {
		return driver.ErrBadConn
	}

	if err := t.c.acquire(); err != nil {
		// If the connection is gone, the transaction is effectively dead.
		if errors.Is(err, driver.ErrBadConn) {
			t.mu.Lock()
			t.done = true
			t.mu.Unlock()
		}
		return err
	}
	defer t.c.release()

	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return sql.ErrTxDone
	}
	t.done = true
	restoreIsolation := t.restoreIsolation
	t.mu.Unlock()

	var opErr error
	if rollback {
		opErr = t.c.sess.rollbackCurrentTransaction(context.Background())
	} else {
		opErr = t.c.sess.commitCurrentTransaction(context.Background())
	}
	if opErr != nil {
		return opErr
	}

	var errs []error
	if restoreIsolation {
		if err := t.c.sess.setTransactionIsolation(context.Background(), defaultTxIsolationSQL); err != nil {
			errs = append(errs, err)
		}
	}

	if err := t.c.sess.setAutoCommit(context.Background(), true); err != nil {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func txIsolationSQL(level driver.IsolationLevel) (sqlLevel string, restoreIsolation bool, err error) {
	switch sql.IsolationLevel(level) {
	case sql.LevelDefault, sql.LevelReadCommitted:
		return "", false, nil
	case sql.LevelReadUncommitted:
		return "READ UNCOMMITTED", true, nil
	case sql.LevelRepeatableRead:
		return "REPEATABLE READ", true, nil
	case sql.LevelSnapshot:
		return "SNAPSHOT", true, nil
	case sql.LevelSerializable:
		return "SERIALIZABLE", true, nil
	case sql.LevelWriteCommitted, sql.LevelLinearizable:
		return "", false, fmt.Errorf("h2go: BeginTx: isolation level %v is not supported by H2", level)
	default:
		return "", false, fmt.Errorf("h2go: BeginTx: unknown isolation level %v", level)
	}
}

func (s *Session) setAutoCommit(ctx context.Context, autoCommit bool) error {
	if s == nil || s.tr == nil {
		return fmt.Errorf("h2go: Session.setAutoCommit: session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.tr.WriteInt32(SessionSetAutocommit); err != nil {
		return fmt.Errorf("h2go: Session.setAutoCommit: failed to write op: %w", err)
	}
	if err := s.tr.WriteBool(autoCommit); err != nil {
		return fmt.Errorf("h2go: Session.setAutoCommit: failed to write autocommit: %w", err)
	}
	if err := s.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: Session.setAutoCommit: flush failed: %w", err)
	}
	if err := readStatus(s.tr); err != nil {
		return wrapError("Session.setAutoCommit", err)
	}
	s.autoCommit = autoCommit
	return nil
}

func (s *Session) commitCurrentTransaction(ctx context.Context) error {
	if s == nil || s.tr == nil {
		return fmt.Errorf("h2go: Session.commitCurrentTransaction: session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.tr.WriteInt32(CommandCommit); err != nil {
		return fmt.Errorf("h2go: Session.commitCurrentTransaction: failed to write op: %w", err)
	}
	if err := s.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: Session.commitCurrentTransaction: flush failed: %w", err)
	}
	if err := readStatus(s.tr); err != nil {
		return wrapError("Session.commitCurrentTransaction", err)
	}
	return nil
}

func (s *Session) rollbackCurrentTransaction(ctx context.Context) error {
	if s == nil || s.tr == nil {
		return fmt.Errorf("h2go: Session.rollbackCurrentTransaction: session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.ExecuteUpdate(ctx, "ROLLBACK")
	if err != nil {
		return wrapError("Session.rollbackCurrentTransaction", err)
	}
	return nil
}

func (s *Session) setTransactionIsolation(ctx context.Context, sqlLevel string) error {
	if s == nil || s.tr == nil {
		return fmt.Errorf("h2go: Session.setTransactionIsolation: session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stmt := "SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL " + sqlLevel
	_, err := s.ExecuteUpdate(ctx, stmt)
	if err != nil {
		return wrapError("Session.setTransactionIsolation", err)
	}
	return nil
}
