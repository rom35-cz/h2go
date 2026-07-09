// command.go — SQL command preparation and remote command handling.
//
// Reference: org.h2.command.CommandRemote, org.h2.engine.SessionRemote

package h2go

import (
	"context"
	"fmt"
)

// PreparedCommand represents a prepared SQL command on the server.
// It holds the command metadata returned by SESSION_PREPARE or
// SESSION_PREPARE_READ_PARAMS2.
type PreparedCommand struct {
	// ID is the server-side command identifier.
	ID int32

	// SQL is the original SQL text.
	SQL string

	// IsQuery is true if this command returns a result set (SELECT, etc.).
	IsQuery bool

	// ReadOnly is true if this command does not modify the database.
	ReadOnly bool

	// CmdType is the command type code (from CommandRemote).
	// Only set when using SESSION_PREPARE_READ_PARAMS2.
	// Values: UNKNOWN=0, SELECT=1, INSERT=2, UPDATE=3, DELETE=4, etc.
	CmdType int32

	// ParamCount is the number of parameters (? placeholders).
	ParamCount int32

	// Params holds parameter metadata when prepared with SESSION_PREPARE_READ_PARAMS2.
	// Each entry contains the type info and nullability for one parameter.
	Params []ParameterMeta
}

// ParameterMeta holds metadata for a single prepared statement parameter.
type ParameterMeta struct {
	// Index is the 0-based parameter position.
	Index int

	// TypeInfo describes the expected parameter type.
	TypeInfo *TypeInfo
}

// Command type constants from CommandRemote.
const (
	CmdUnknown        = 0
	CmdSelect         = 1
	CmdInsert         = 2
	CmdUpdate         = 3
	CmdDelete         = 4
	CmdCreateTable    = 5
	CmdDropTable      = 6
	CmdCreateIndex    = 7
	CmdDropIndex      = 8
	CmdSelectDual     = 9
	CmdAlterTable     = 10
	CmdTruncateTable  = 11
	CmdCommit         = 12
	CmdRollback       = 13
	CmdGrant          = 14
	CmdRevoke         = 15
	CmdPrepare        = 16
	CmdExecute        = 17
	CmdComment        = 18
	CmdSavepoint      = 19
	CmdProcedure      = 20
	CmdExplain        = 21
	CmdMerge          = 22
	CmdShow           = 23
	CmdAlterIndex     = 24
	CmdRunScript      = 25
	CmdScript         = 26
	CmdShutdown       = 27
	CmdCheckpoint     = 28
	CmdExplainAnalyze = 29
)

// PrepareCommand prepares a SQL statement on the server.
// It sends SESSION_PREPARE and reads the command metadata.
// This is the minimal preparation that doesn't read parameter metadata.
//
// For prepared statements that need parameter type information (for
// NumInput and parameter encoding), use PrepareCommandReadParams instead.
func (s *Session) PrepareCommand(ctx context.Context, sql string) (*PreparedCommand, error) {
	if s.tr == nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: session closed")
	}

	// Generate a new command ID
	cmdID := s.nextCommandID()

	// Write SESSION_PREPARE: [op, id, sql]
	if err := s.tr.WriteInt32(SessionPrepare); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to write op: %w", err)
	}
	if err := s.tr.WriteInt32(cmdID); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to write command id: %w", err)
	}
	if err := s.tr.WriteString(sql); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to write SQL: %w", err)
	}

	// Flush and check for context cancellation
	if err := s.tr.Flush(); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: flush failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Read response.
	// Server: writeInt(status) . writeBoolean(isQuery) . writeBoolean(readOnly) . writeInt(paramCount)
	// Check status first — if STATUS_ERROR the server follows with an H2Error payload.
	if err := readStatus(s.tr); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: %w", err)
	}

	isQuery, err := s.tr.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to read isQuery: %w", err)
	}

	readOnly, err := s.tr.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to read readOnly: %w", err)
	}

	paramCount, err := s.tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommand: failed to read paramCount: %w", err)
	}

	cmd := &PreparedCommand{
		ID:         cmdID,
		SQL:        sql,
		IsQuery:    isQuery,
		ReadOnly:   readOnly,
		CmdType:    CmdUnknown,
		ParamCount: paramCount,
		Params:     nil, // Not populated by SESSION_PREPARE
	}

	return cmd, nil
}

// PrepareCommandReadParams prepares a SQL statement and reads parameter metadata.
// It sends SESSION_PREPARE_READ_PARAMS2 which returns:
//   - isQuery (boolean)
//   - readOnly (boolean)
//   - cmdType (int) - the command type
//   - paramCount (int)
//   - for each parameter: TypeInfo, nullable (int)
//
// This is used for driver.Stmt.NumInput and for determining parameter types
// before binding values.
func (s *Session) PrepareCommandReadParams(ctx context.Context, sql string) (*PreparedCommand, error) {
	if s.tr == nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: session closed")
	}

	// Generate a new command ID
	cmdID := s.nextCommandID()

	// Write SESSION_PREPARE_READ_PARAMS2: [op, id, sql]
	if err := s.tr.WriteInt32(SessionPrepareReadParams2); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to write op: %w", err)
	}
	if err := s.tr.WriteInt32(cmdID); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to write command id: %w", err)
	}
	if err := s.tr.WriteString(sql); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to write SQL: %w", err)
	}

	// Flush and check for context cancellation
	if err := s.tr.Flush(); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: flush failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Read response
	// Server sends: status, isQuery (boolean), readOnly (boolean), cmdType (int), paramCount (int)
	if err := readStatus(s.tr); err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: %w", err)
	}

	isQuery, err := s.tr.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read isQuery: %w", err)
	}

	readOnly, err := s.tr.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read readOnly: %w", err)
	}

	cmdType, err := s.tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read cmdType: %w", err)
	}

	paramCount, err := s.tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read paramCount: %w", err)
	}

	// Read parameter metadata
	params := make([]ParameterMeta, paramCount)
	for i := 0; i < int(paramCount); i++ {
		// Read TypeInfo for this parameter
		typeInfo, err := s.tr.ReadTypeInfo()
		if err != nil {
			return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read param %d type: %w", i, err)
		}

		// Read nullable status
		nullable, err := s.tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: PrepareCommandReadParams: failed to read param %d nullable: %w", i, err)
		}
		typeInfo.Nullable = int(nullable)

		params[i] = ParameterMeta{
			Index:    i,
			TypeInfo: typeInfo,
		}
	}

	cmd := &PreparedCommand{
		ID:         cmdID,
		SQL:        sql,
		IsQuery:    isQuery,
		ReadOnly:   readOnly,
		CmdType:    cmdType,
		ParamCount: paramCount,
		Params:     params,
	}

	return cmd, nil
}

// Close sends COMMAND_CLOSE to release the server-side command.
// The command ID becomes invalid after this call.
func (cmd *PreparedCommand) Close(s *Session) error {
	if s == nil || s.tr == nil {
		return nil // Already closed or session unavailable
	}

	// Write COMMAND_CLOSE: [op, id]
	if err := s.tr.WriteInt32(CommandClose); err != nil {
		return fmt.Errorf("h2go: PreparedCommand.Close: failed to write op: %w", err)
	}
	if err := s.tr.WriteInt32(cmd.ID); err != nil {
		return fmt.Errorf("h2go: PreparedCommand.Close: failed to write command id: %w", err)
	}

	if err := s.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: PreparedCommand.Close: flush failed: %w", err)
	}

	// COMMAND_CLOSE does not return a response in protocol 21
	// Server immediately frees the command
	return nil
}

// nextCommandID generates a unique command ID for this session.
// This is a simple incrementing counter (like Java SessionRemote.getNextId()).
func (s *Session) nextCommandID() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}

// CommandTypeName returns a human-readable name for a command type constant.
func CommandTypeName(cmdType int32) string {
	switch cmdType {
	case CmdUnknown:
		return "UNKNOWN"
	case CmdSelect:
		return "SELECT"
	case CmdInsert:
		return "INSERT"
	case CmdUpdate:
		return "UPDATE"
	case CmdDelete:
		return "DELETE"
	case CmdCreateTable:
		return "CREATE TABLE"
	case CmdDropTable:
		return "DROP TABLE"
	case CmdCreateIndex:
		return "CREATE INDEX"
	case CmdDropIndex:
		return "DROP INDEX"
	case CmdSelectDual:
		return "SELECT DUAL"
	case CmdAlterTable:
		return "ALTER TABLE"
	case CmdTruncateTable:
		return "TRUNCATE TABLE"
	case CmdCommit:
		return "COMMIT"
	case CmdRollback:
		return "ROLLBACK"
	case CmdGrant:
		return "GRANT"
	case CmdRevoke:
		return "REVOKE"
	case CmdPrepare:
		return "PREPARE"
	case CmdExecute:
		return "EXECUTE"
	case CmdComment:
		return "COMMENT"
	case CmdSavepoint:
		return "SAVEPOINT"
	case CmdProcedure:
		return "PROCEDURE"
	case CmdExplain:
		return "EXPLAIN"
	case CmdMerge:
		return "MERGE"
	case CmdShow:
		return "SHOW"
	case CmdAlterIndex:
		return "ALTER INDEX"
	case CmdRunScript:
		return "RUNSCRIPT"
	case CmdScript:
		return "SCRIPT"
	case CmdShutdown:
		return "SHUTDOWN"
	case CmdCheckpoint:
		return "CHECKPOINT"
	case CmdExplainAnalyze:
		return "EXPLAIN ANALYZE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", cmdType)
	}
}
