package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
)

// ResultWithUpdateCount captures the result of an executeUpdate operation.
type ResultWithUpdateCount struct {
	UpdateCount int64
	AutoCommit  bool
}

// Session holds an authenticated H2 TCP session.
type Session struct {
	tr         *Tr
	id         string
	version    int32
	autoCommit bool
	mu         sync.Mutex // protects nextID
	nextID     int32      // incremented for each command ID
}

// Close sends a graceful SESSION_CLOSE notification to the server and closes
// the underlying TCP connection.
//
// Following the H2 TCP protocol, the client sends the SessionClose operation
// (op=1) and reads the server's STATUS_OK reply before closing the socket.
// This prevents the server from logging "Connection is broken" on every clean
// session termination.
//
// All intermediate errors (write, flush, read) are best-effort: they are
// discarded so that a broken-connection close still completes. Future phases
// may add a configurable deadline on the STATUS_OK read.
func (s *Session) Close() error {
	if s.tr == nil {
		return nil
	}
	tr := s.tr
	s.tr = nil // prevent double-close

	// Notify the server and drain its STATUS_OK reply. Errors are discarded:
	// the transport must be closed regardless of the session state.
	_ = tr.WriteInt32(SessionClose)
	_ = tr.Flush()
	_, _ = tr.ReadInt32() // STATUS_OK; ignore value and error

	return tr.Close()
}

// hasPendingTransaction asks the server whether the current session has
// uncommitted changes. It is used by the validator and session reset path
// to make a low-cost round trip without preparing SQL text.
func (s *Session) hasPendingTransaction(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.tr == nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: session closed")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.tr.WriteInt32(SessionHasPendingTransaction); err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: failed to write op: %w", err)
	}
	if err := s.tr.Flush(); err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: flush failed: %w", err)
	}
	if err := readStatus(s.tr); err != nil {
		return false, wrapError("Session.hasPendingTransaction", err)
	}
	pending, err := s.tr.ReadInt32()
	if err != nil {
		return false, fmt.Errorf("h2go: Session.hasPendingTransaction: failed to read pending flag: %w", err)
	}
	return pending != 0, nil
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
	res, err := s.ExecuteUpdatePrepared(cmd)
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

	res, err := s.ExecuteUpdatePreparedWithParams(cmd, params)
	if err != nil {
		_ = cmd.Close(s)
		return nil, err
	}

	_ = cmd.Close(s) // best-effort; ignore close errors
	return res, nil
}

// ExecuteUpdatePrepared executes a pre-prepared update command without params.
func (s *Session) ExecuteUpdatePrepared(cmd *PreparedCommand) (*ResultWithUpdateCount, error) {
	return s.ExecuteUpdatePreparedWithParams(cmd, nil)
}

// ExecuteUpdatePreparedWithParams executes a pre-prepared update command with
// positional parameters.
func (s *Session) ExecuteUpdatePreparedWithParams(cmd *PreparedCommand, params []driver.Value) (*ResultWithUpdateCount, error) {
	if s == nil || s.tr == nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "session closed")
	}
	if cmd == nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "nil command")
	}
	if cmd.IsQuery {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", "command is a query, not an update")
	}

	if int(cmd.ParamCount) != len(params) {
		return nil, fmt.Errorf("h2go: ExecuteUpdatePreparedWithParams: expected %d params, got %d",
			cmd.ParamCount, len(params))
	}

	// Send COMMAND_EXECUTE_UPDATE
	if err := s.tr.WriteInt32(CommandExecuteUpdate); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}
	if err := s.tr.WriteInt32(cmd.ID); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	// Send parameter count and parameter values.
	if err := s.tr.WriteInt32(int32(len(params))); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}
	for i, param := range params {
		var paramType *TypeInfo
		if i < len(cmd.Params) {
			paramType = cmd.Params[i].TypeInfo
		}
		if err := s.tr.WriteValue(param, paramType); err != nil {
			return nil, fmt.Errorf("h2go: ExecuteUpdatePreparedWithParams: encode param %d: %w", i+1, err)
		}
	}

	// Send generated keys mode: NONE = 0 for MVP.
	if err := s.tr.WriteInt32(GeneratedKeysNone); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	if err := s.tr.Flush(); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	// Read response: status, updateCount (rowCount), autoCommit.
	// Server: writeInt(status) . writeRowCount(updateCount) . writeBoolean(autoCommit)
	if err := readStatus(s.tr); err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	updateCount, err := s.tr.ReadRowCount()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	autoCommit, err := s.tr.ReadBool()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePreparedWithParams", err)
	}

	s.autoCommit = autoCommit

	return &ResultWithUpdateCount{
		UpdateCount: updateCount,
		AutoCommit:  autoCommit,
	}, nil
}
