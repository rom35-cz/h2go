package h2go

import (
	"context"
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

// ExecuteUpdate executes an update statement (INSERT, UPDATE, DELETE, MERGE)
// and returns the affected row count. For MVP, this only supports
// parameterless queries; parameterized queries require Phase 6 (T6.x).
//
// This method prepares the command, executes it, and closes the command.
// Use ExecuteUpdatePrepared for reusable prepared statements.
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

// ExecuteUpdatePrepared executes a pre-prepared update command.
// For MVP, this only supports parameterless queries.
func (s *Session) ExecuteUpdatePrepared(cmd *PreparedCommand) (*ResultWithUpdateCount, error) {
	if cmd.IsQuery {
		return nil, wrapError("ExecuteUpdatePrepared", "command is a query, not an update")
	}

	// Send COMMAND_EXECUTE_UPDATE
	if err := s.tr.WriteInt32(CommandExecuteUpdate); err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}
	if err := s.tr.WriteInt32(cmd.ID); err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	// Send parameter count (0 for MVP parameterless queries)
	if err := s.tr.WriteInt32(0); err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	// Send generated keys mode: NONE = 0 for MVP
	if err := s.tr.WriteInt32(GeneratedKeysNone); err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	if err := s.tr.Flush(); err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	// Read response: status, updateCount (rowCount), autoCommit.
	// Server: writeInt(status) . writeRowCount(updateCount) . writeBoolean(autoCommit)
	if err := readStatus(s.tr); err != nil {
		return nil, fmt.Errorf("h2go: ExecuteUpdatePrepared: %w", err)
	}

	updateCount, err := s.tr.ReadRowCount()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	autoCommit, err := s.tr.ReadBool()
	if err != nil {
		return nil, wrapError("ExecuteUpdatePrepared", err)
	}

	s.autoCommit = autoCommit

	return &ResultWithUpdateCount{
		UpdateCount: updateCount,
		AutoCommit:  autoCommit,
	}, nil
}
