package h2go

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ResultWithUpdateCount captures the result of an executeUpdate operation.
type ResultWithUpdateCount struct {
	UpdateCount int64
	AutoCommit  bool

	LastInsertID    int64
	LastInsertIDSet bool
	LastInsertErr   error

	// GeneratedKeys holds the full generated-keys result when available.
	// For statements that return generated keys (INSERT, MERGE), this field
	// provides access to multi-column and multi-row key values.
	GeneratedKeys *GeneratedKeysResult
}

// Session holds an authenticated H2 TCP session.
type Session struct {
	tr         *Tr
	cfg        *Config
	id         string
	version    int32
	autoCommit bool
	dead       atomic.Bool // atomically set by Close/Abort; read by conn.acquire
	mu         sync.Mutex  // protects nextID
	nextID     int32       // incremented for each command ID
}

// closeStatusTimeout bounds how long Close waits for the server's STATUS_OK
// reply after sending SESSION_CLOSE. A half-open or dead peer cannot stall
// pool teardown beyond this deadline.
const closeStatusTimeout = 2 * time.Second

// Close sends a graceful SESSION_CLOSE notification to the server and closes
// the underlying TCP connection.
//
// Following the H2 TCP protocol, the client sends the SessionClose operation
// (op=1) and reads the server's STATUS_OK reply before closing the socket.
// This prevents the server from logging "Connection is broken" on every clean
// session termination.
//
// All intermediate errors (write, flush, read) are best-effort: they are
// discarded so that a broken-connection close still completes. The STATUS_OK
// read is bounded by closeStatusTimeout so a dead or half-open peer cannot
// stall the close indefinitely.
func (s *Session) Close() error {
	if s.tr == nil {
		s.dead.Store(true)
		return nil
	}
	tr := s.tr
	s.tr = nil // prevent double-close
	s.dead.Store(true)

	// Notify the server and drain its STATUS_OK reply under a short deadline.
	// Errors are discarded: the transport must be closed regardless of the
	// session state.
	_ = tr.SetDeadline(time.Now().Add(closeStatusTimeout))
	_ = tr.WriteInt32(SessionClose)
	_ = tr.Flush()
	_, _ = tr.ReadInt32() // STATUS_OK; ignore value and error
	_ = tr.SetDeadline(time.Time{})

	return tr.Close()
}

// Abort closes the underlying transport immediately without sending the
// protocol-level SESSION_CLOSE handshake. It is used after context cancellation
// or deadline expiry to ensure the driver never reuses an unknown session.
func (s *Session) Abort() error {
	if s == nil || s.tr == nil {
		if s != nil {
			s.dead.Store(true)
		}
		return nil
	}
	tr := s.tr
	s.tr = nil
	s.dead.Store(true)
	return tr.Abort()
}

// markStreamBroken records that the transfer stream can no longer be trusted
// — a mid-frame parse failure, a partially written operation, or a transport
// timeout left unread bytes or an unusable pipe — and aborts the transport.
// ResetSession/acquire then report driver.ErrBadConn, so database/sql
// discards the connection instead of reusing one whose stream position is
// unknown. Behaviourally identical to Abort; named for intent at row-reader
// call sites.
func (s *Session) markStreamBroken() {
	if s == nil {
		return
	}
	_ = s.Abort()
}

// markUnlessAlignedH2Error marks the stream broken unless err is a fully
// parsed server-side H2 error (*Error). An H2 error frame is consumed
// completely by readH2Error, leaving the stream aligned: per H2 semantics a
// server-side SQL error ends the current result, not the session. Anything
// else (EOF, timeout, truncated frame) means the stream position is unknown.
func (s *Session) markUnlessAlignedH2Error(err error) {
	if s == nil || err == nil {
		return
	}
	var he *Error
	if !errors.As(err, &he) {
		s.markStreamBroken()
	}
}

// cancelStatement uses H2's side-channel cancellation protocol to ask the
// server to stop a running statement identified by statementID.
func (s *Session) cancelStatement(statementID int32) error {
	if s == nil || s.cfg == nil {
		return fmt.Errorf("h2go: Session.cancelStatement: missing connection config")
	}
	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: dial %s: %w", addr, err)
	}
	tr := NewReadWriter(conn)
	defer func() { _ = tr.Abort() }()

	if err := tr.WriteInt32(TCPProtocolVersionMinSupported); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write min version: %w", err)
	}
	if err := tr.WriteInt32(TCPProtocolVersionMaxSupported); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write max version: %w", err)
	}
	if err := tr.WriteNullString(); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write db: %w", err)
	}
	if err := tr.WriteNullString(); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write original url: %w", err)
	}
	if err := tr.WriteString(s.id); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write session id: %w", err)
	}
	if err := tr.WriteInt32(SessionCancelStatement); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write cancel op: %w", err)
	}
	if err := tr.WriteInt32(statementID); err != nil {
		return fmt.Errorf("h2go: Session.cancelStatement: write statement id: %w", err)
	}
	return tr.Close()
}

func (s *Session) finalizeContext(ctx context.Context, errp *error) {
	if errp == nil || *errp == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		_ = s.Abort()
		*errp = ctx.Err()
		return
	}
	if ctx == nil {
		return
	}
	// The transport deadline is set to the context deadline by
	// beginOperationContext, so an I/O timeout can surface a few microseconds
	// BEFORE the context timer's callback marks the context done. Detect that
	// window deterministically: a timeout-kind transport error observed after
	// the context deadline has passed is the context deadline, and must abort
	// the session just like the ctx.Err() path above.
	deadline, ok := ctx.Deadline()
	if !ok {
		return
	}
	var ne net.Error
	if errors.As(*errp, &ne) && ne.Timeout() && time.Now().After(deadline) {
		_ = s.Abort()
		*errp = context.DeadlineExceeded
	}
}

// hasPendingTransaction asks the server whether the current session has
// uncommitted changes. It is used by the validator and session reset path
// to make a low-cost round trip without preparing SQL text.
func (s *Session) hasPendingTransaction(ctx context.Context) (pending bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.tr == nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: session closed")
	}
	cleanup := beginOperationContext(ctx, s.tr, nil)
	defer cleanup()
	defer s.finalizeContext(ctx, &err)

	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = s.tr.WriteInt32(SessionHasPendingTransaction); err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: failed to write op: %w", err)
	}
	if err = s.tr.Flush(); err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: flush failed: %w", err)
	}
	if err = readStatus(s.tr); err != nil {
		return false, wrapError("Session.hasPendingTransaction", err)
	}
	var pendingRaw int32
	pendingRaw, err = s.tr.ReadInt32()
	if err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: failed to read pending flag: %w", err)
	}
	return pendingRaw != 0, nil
}

// ExecuteUpdate executes a parameterless update statement (INSERT, UPDATE,
// DELETE, MERGE) and returns the affected row count.
//
// This method prepares the command, executes it, and closes the command.
// For parameterized execution, use ExecuteUpdateWithParams.
func (s *Session) ExecuteUpdate(ctx context.Context, sql string) (*ResultWithUpdateCount, error) {
	// Prepare the command
	cmd, err := s.PrepareCommand(ctx, sql)
	if err != nil {
		return nil, err
	}

	// Execute the update
	res, err := s.ExecuteUpdatePrepared(ctx, cmd)
	if err != nil {
		// Attempt to close command even on error
		_ = cmd.Close(s)
		return nil, err
	}

	// Close the ad-hoc command
	_ = cmd.Close(s) // best-effort; ignore close errors

	return res, nil
}

// ExecuteUpdateWithParams executes an update statement with positional
// parameter values and returns the affected row count.
//
// It prepares with SESSION_PREPARE_READ_PARAMS2 to obtain parameter metadata
// and encodes each value with WriteValue using that metadata.
func (s *Session) ExecuteUpdateWithParams(ctx context.Context, sql string, params []driver.Value) (*ResultWithUpdateCount, error) {
	cmd, err := s.PrepareCommandReadParams(ctx, sql)
	if err != nil {
		return nil, err
	}

	res, err := s.ExecuteUpdatePreparedWithParams(ctx, cmd, params)
	if err != nil {
		_ = cmd.Close(s)
		return nil, err
	}

	_ = cmd.Close(s) // best-effort; ignore close errors
	return res, nil
}

// ExecuteUpdatePrepared executes a pre-prepared update command without params.
func (s *Session) ExecuteUpdatePrepared(ctx context.Context, cmd *PreparedCommand) (*ResultWithUpdateCount, error) {
	return s.ExecuteUpdatePreparedWithParams(ctx, cmd, nil)
}

// ExecuteUpdatePreparedWithParams executes a pre-prepared update command with
// positional parameters.
func (s *Session) ExecuteUpdatePreparedWithParams(ctx context.Context, cmd *PreparedCommand, params []driver.Value) (res *ResultWithUpdateCount, err error) {
	if s == nil || s.tr == nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "session closed")
	}
	if cmd == nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "nil command")
	}
	if cmd.IsQuery {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "command is a query, not an update")
	}

	cleanup := beginOperationContext(ctx, s.tr, func() error { return s.cancelStatement(cmd.ID) })
	defer cleanup()
	defer s.finalizeContext(ctx, &err)

	if int(cmd.ParamCount) != len(params) {
		return nil, fmt.Errorf("h2go: ExecuteUpdatePreparedWithParams: expected %d params, got %d",
			cmd.ParamCount, len(params))
	}

	// Send COMMAND_EXECUTE_UPDATE
	if err = s.tr.WriteInt32(CommandExecuteUpdate); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}
	if err = s.tr.WriteInt32(cmd.ID); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	// Send parameter count and parameter values.
	if err = s.tr.WriteInt32(int32(len(params))); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}
	for i, param := range params {
		var paramType *TypeInfo
		if i < len(cmd.Params) {
			paramType = cmd.Params[i].TypeInfo
		}
		if err = s.tr.WriteValue(param, paramType); err != nil {
			return nil, fmt.Errorf("h2go: ExecuteUpdatePreparedWithParams: encode param %d: %w", i+1, err)
		}
	}

	// Request generated keys so Result.LastInsertId can be populated when H2
	// returns exactly one numeric generated key. The helper below will consume
	// the generated-keys result and convert it into a driver.Result field.
	genKeysMode := s.generatedKeysMode()
	if err = s.tr.WriteInt32(genKeysMode); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	// For column-number and column-name modes, write the selector data.
	switch genKeysMode {
	case GeneratedKeysColumnNumbers:
		cols := s.cfg.GeneratedKeysColumns
		if cols == nil {
			cols = []int{}
		}
		if err = s.tr.WriteInt32(int32(len(cols))); err != nil {
			return nil, wrapError("ExecuteUpdatePreparedWithParams: write column count", err)
		}
		for _, col := range cols {
			if err = s.tr.WriteInt32(int32(col)); err != nil {
				return nil, wrapError("ExecuteUpdatePreparedWithParams: write column index", err)
			}
		}

	case GeneratedKeysColumnNames:
		names := s.cfg.GeneratedKeysColumnNames
		if names == nil {
			names = []string{}
		}
		if err = s.tr.WriteInt32(int32(len(names))); err != nil {
			return nil, wrapError("ExecuteUpdatePreparedWithParams: write name count", err)
		}
		for _, name := range names {
			if err = s.tr.WriteString(name); err != nil {
				return nil, wrapError("ExecuteUpdatePreparedWithParams: write column name", err)
			}
		}
	}

	if err = s.tr.Flush(); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	// Read response: status, updateCount (rowCount), autoCommit.
	// Server: writeInt(status) . writeRowCount(updateCount) . writeBoolean(autoCommit)
	if err = readStatus(s.tr); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	var updateCount int64
	updateCount, err = s.tr.ReadRowCount()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	var autoCommit bool
	autoCommit, err = s.tr.ReadBool()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	lastInsertErr := error(nil)
	var generatedKeys *GeneratedKeysResult
	var lastInsertID int64
	lastInsertIDSet := false
	if genKeysMode != GeneratedKeysNone {
		generatedKeys, lastInsertID, err = s.readGeneratedKeys()
		if err != nil {
			if isLastInsertIDUnavailable(err) {
				lastInsertErr = err
			} else {
				return nil, err
			}
		} else {
			lastInsertIDSet = true
		}
	}

	s.autoCommit = autoCommit

	return &ResultWithUpdateCount{
		UpdateCount:     updateCount,
		AutoCommit:      autoCommit,
		LastInsertID:    lastInsertID,
		LastInsertIDSet: lastInsertIDSet,
		LastInsertErr:   lastInsertErr,
		GeneratedKeys:   generatedKeys,
	}, nil
}

// generatedKeysMode returns the generated keys mode to use for this session.
// It reads from the config, defaulting to GeneratedKeysAuto when the caller
// has not explicitly set GeneratedKeysMode.
func (s *Session) generatedKeysMode() int32 {
	if s == nil || s.cfg == nil || !s.cfg.GeneratedKeysModeSet {
		return GeneratedKeysAuto
	}
	switch s.cfg.GeneratedKeysMode {
	case GeneratedKeysNone, GeneratedKeysAuto, GeneratedKeysColumnNumbers, GeneratedKeysColumnNames:
		return int32(s.cfg.GeneratedKeysMode)
	default:
		return GeneratedKeysAuto
	}
}
