// rows.go — driver.Rows implementation for H2 result sets.
//
// Reference: org.h2.result.ResultRemote, driver.Rows interface

package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
)

// Rows implements driver.Rows for H2 result sets.
type Rows struct {
	// session is the underlying H2 session.
	session *Session

	// resultID is the server-side result set identifier.
	resultID int32

	// columnCount is the number of columns in the result.
	columnCount int32

	// columns contains the result column metadata.
	columns *ResultMeta

	// rowCount is the total number of rows (-1 for lazy/large results).
	rowCount int64

	// currentRow is the current row data.
	currentRow []driver.Value

	// rowOffset is the index of the first row in the buffered rows.
	rowOffset int64

	// bufferedRows holds prefetched rows.
	bufferedRows [][]driver.Value

	// fetchSize is the number of rows to fetch per request.
	fetchSize int

	// noMoreRows is set when the server sends the end-of-result flag (byte 0)
	// during row fetching, indicating the result set is closed server-side.
	// Once set, no further RESULT_FETCH_ROWS requests must be sent.
	noMoreRows bool

	// closed indicates if the result set has been closed.
	closed bool

	// ownsCommand indicates if this Rows owns the command and should close it.
	// True for ad-hoc queries, false for prepared statement results.
	ownsCommand bool

	// commandID is the owning command ID (for closing if ownsCommand is true).
	commandID int32

	// err holds any error that occurred during operation.
	err error

	// closeCallback is called after the rows are closed.
	// Used by conn.QueryContext to release the connection lock; prepared
	// statement rows may also use it to perform deferred COMMAND_CLOSE.
	closeCallback func() error
}

// NewRows creates a new Rows instance for reading query results.
// This is called after COMMAND_EXECUTE_QUERY returns the column count.
// The columnCount is already read; this function reads the row count and column metadata.
func NewRows(session *Session, resultID int32, columnCount int32, fetchSize int, ownsCommand bool, commandID int32) (*Rows, error) {
	if session == nil || session.tr == nil {
		return nil, fmt.Errorf("h2go: NewRows: nil session or closed transport")
	}

	r := &Rows{
		session:     session,
		resultID:    resultID,
		columnCount: columnCount,
		fetchSize:   fetchSize,
		rowCount:    -1, // Will be read from stream
		ownsCommand: ownsCommand,
		commandID:   commandID,
	}

	// Read row count
	rowCount, err := session.tr.ReadRowCount()
	if err != nil {
		return nil, fmt.Errorf("h2go: NewRows: failed to read row count: %w", err)
	}
	r.rowCount = rowCount

	// Read column metadata
	meta, err := session.tr.ReadResultMeta(columnCount, TCPProtocolVersion21)
	if err != nil {
		return nil, fmt.Errorf("h2go: NewRows: failed to read column metadata: %w", err)
	}
	r.columns = meta

	// Prefetch initial rows
	if err := r.prefetchInitialRows(); err != nil {
		return nil, err
	}

	return r, nil
}

// prefetchInitialRows reads the initial batch of rows.
func (r *Rows) prefetchInitialRows() error {
	// Determine how many rows to fetch initially
	fetch := r.fetchSize
	if r.rowCount >= 0 && int64(fetch) > r.rowCount {
		fetch = int(r.rowCount)
	}

	if fetch > 0 {
		if err := r.fetchRows(fetch); err != nil {
			return fmt.Errorf("h2go: prefetchInitialRows: %w", err)
		}
	}

	return nil
}

// Columns returns the column names.
// This implements driver.Rows.Columns.
func (r *Rows) Columns() []string {
	if r.columns == nil {
		return nil
	}
	return r.columns.ColumnNames()
}

// Close closes the result set and frees server resources.
// This implements driver.Rows.Close.
//
// If this Rows owns the command (ad-hoc query), the command is also closed.
// If the result set is backed by a prepared statement, only the result is closed.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	var closeErr error

	// Close the result on the server
	if r.session != nil && r.session.tr != nil {
		if err := r.sendResultClose(); err != nil {
			closeErr = err
		}
	}

	// If we own the command, close it too (for ad-hoc queries)
	if r.ownsCommand && r.commandID != 0 && r.session != nil && r.session.tr != nil {
		if err := r.sendCommandClose(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	// Clear references
	r.bufferedRows = nil
	r.currentRow = nil
	r.columns = nil

	// Call the close callback if set (used to release connection lock and,
	// for prepared statements, to perform any deferred statement close).
	if r.closeCallback != nil {
		if err := r.closeCallback(); err != nil && closeErr == nil {
			closeErr = err
		}
		r.closeCallback = nil
	}

	return closeErr
}

// sendResultClose sends RESULT_CLOSE to the server.
func (r *Rows) sendResultClose() error {
	if err := r.session.tr.WriteInt32(ResultClose); err != nil {
		return fmt.Errorf("h2go: Rows.sendResultClose: failed to write op: %w", err)
	}
	if err := r.session.tr.WriteInt32(r.resultID); err != nil {
		return fmt.Errorf("h2go: Rows.sendResultClose: failed to write result id: %w", err)
	}
	if err := r.session.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: Rows.sendResultClose: flush failed: %w", err)
	}
	return nil
}

// sendCommandClose sends COMMAND_CLOSE to the server.
func (r *Rows) sendCommandClose() error {
	if err := r.session.tr.WriteInt32(CommandClose); err != nil {
		return fmt.Errorf("h2go: Rows.sendCommandClose: failed to write op: %w", err)
	}
	if err := r.session.tr.WriteInt32(r.commandID); err != nil {
		return fmt.Errorf("h2go: Rows.sendCommandClose: failed to write command id: %w", err)
	}
	if err := r.session.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: Rows.sendCommandClose: flush failed: %w", err)
	}
	return nil
}

// Next reads the next row into dest.
// This implements driver.Rows.Next.
// Returns io.EOF when there are no more rows.
func (r *Rows) Next(dest []driver.Value) error {
	if r.closed {
		// A closed result set has no more rows. Returning io.EOF (rather than
		// driver.ErrBadConn) avoids falsely poisoning the connection pool.
		return io.EOF
	}
	if r.err != nil {
		return r.err
	}

	// Ensure dest is the right length
	if len(dest) != int(r.columnCount) {
		return fmt.Errorf("h2go: Rows.Next: dest length %d does not match column count %d",
			len(dest), r.columnCount)
	}

	// Check if we need to fetch more rows
	if len(r.bufferedRows) == 0 {
		if r.rowCount >= 0 && r.rowOffset >= r.rowCount {
			return io.EOF
		}
		if r.noMoreRows {
			return io.EOF
		}

		// Need to fetch more rows
		fetch := r.fetchSize
		if r.rowCount >= 0 {
			remaining := r.rowCount - r.rowOffset
			if remaining <= 0 {
				return io.EOF
			}
			if int64(fetch) > remaining {
				fetch = int(remaining)
			}
		}

		if err := r.fetchMoreRows(fetch); err != nil {
			// Make the error sticky: the read stream may now be misaligned, so a
			// subsequent Next must not issue another RESULT_FETCH_ROWS.
			r.err = err
			return err
		}

		// Check again after fetching
		if len(r.bufferedRows) == 0 {
			return io.EOF
		}
	}

	// Return the next row
	r.currentRow = r.bufferedRows[0]
	r.bufferedRows = r.bufferedRows[1:]
	r.rowOffset++

	// Copy to dest
	copy(dest, r.currentRow)

	return nil
}

// fetchMoreRows sends RESULT_FETCH_ROWS and reads the rows.
func (r *Rows) fetchMoreRows(fetch int) error {
	if r.session == nil || r.session.tr == nil {
		return driver.ErrBadConn
	}

	// Send RESULT_FETCH_ROWS
	if err := r.session.tr.WriteInt32(ResultFetchRows); err != nil {
		return fmt.Errorf("h2go: Rows.fetchMoreRows: failed to write op: %w", err)
	}
	if err := r.session.tr.WriteInt32(r.resultID); err != nil {
		return fmt.Errorf("h2go: Rows.fetchMoreRows: failed to write result id: %w", err)
	}
	if err := r.session.tr.WriteInt32(int32(fetch)); err != nil {
		return fmt.Errorf("h2go: Rows.fetchMoreRows: failed to write fetch size: %w", err)
	}
	if err := r.session.tr.Flush(); err != nil {
		return fmt.Errorf("h2go: Rows.fetchMoreRows: flush failed: %w", err)
	}

	// Server responds: writeInt(STATUS_OK) then row bytes (sendRows).
	if err := readStatus(r.session.tr); err != nil {
		return wrapError("Rows.fetchMoreRows", err)
	}

	// Read the rows
	return r.fetchRows(fetch)
}

// fetchRows reads up to 'fetch' rows from the transfer stream.
// The wire format for each row is:
//   - byte 1: row data follows, read each column value
//   - byte 0: end of result (no more rows)
//   - byte -1: exception/error
func (r *Rows) fetchRows(fetch int) error {
	for i := 0; i < fetch; i++ {
		rowFlag, err := r.session.tr.ReadByte()
		if err != nil {
			return fmt.Errorf("h2go: fetchRows: failed to read row flag: %w", err)
		}

		switch int8(rowFlag) {
		case 1:
			// Row data follows
			row := make([]driver.Value, r.columnCount)
			for j := 0; j < int(r.columnCount); j++ {
				colType := r.columns.GetColumn(j)
				if colType == nil {
					return fmt.Errorf("h2go: fetchRows: column %d metadata not found", j)
				}
				value, err := r.session.tr.ReadValue(colType.TypeInfo)
				if err != nil {
					return fmt.Errorf("h2go: fetchRows: failed to read column %d: %w", j, err)
				}
				row[j] = value
			}
			r.bufferedRows = append(r.bufferedRows, row)

		case 0:
			// End of result — server has already closed the result set.
			// Record this so Next() does not send another RESULT_FETCH_ROWS.
			r.noMoreRows = true
			return nil

		case -1:
			// Exception
			h2Err := readH2Error(r.session.tr)
			if h2Err == nil {
				return fmt.Errorf("h2go: fetchRows: expected H2 error but got nil")
			}
			return h2Err

		default:
			return fmt.Errorf("h2go: fetchRows: unexpected row flag %d", rowFlag)
		}
	}

	return nil
}

// HasNextResultSet is not supported in MVP.
func (r *Rows) HasNextResultSet() bool {
	return false
}

// NextResultSet is not supported in MVP.
func (r *Rows) NextResultSet() error {
	return fmt.Errorf("h2go: multiple result sets not supported in MVP")
}

// ColumnTypeDatabaseTypeName returns the database type name for a column.
// This is part of the driver.RowsColumnTypeDatabaseTypeName interface (optional).
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	col := r.columns.GetColumn(index)
	if col == nil || col.TypeInfo == nil {
		return ""
	}
	return col.TypeInfo.DatabaseTypeName()
}

// ColumnTypeNullable returns the nullability for a column.
// This is part of the driver.RowsColumnTypeNullable interface (optional).
func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	col := r.columns.GetColumn(index)
	if col == nil {
		return false, false
	}
	switch col.Nullable {
	case ColumnNoNulls:
		return false, true
	case ColumnNullable:
		return true, true
	default:
		return false, false // unknown
	}
}

// ColumnTypeLength returns the length for variable-length types.
// This is part of the driver.RowsColumnTypeLength interface (optional).
func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	col := r.columns.GetColumn(index)
	if col == nil || col.TypeInfo == nil {
		return 0, false
	}

	switch col.TypeInfo.ValueType {
	case ValueTypeVarchar, ValueTypeChar, ValueTypeVarcharIgnoreCase,
		ValueTypeBinary, ValueTypeVarbinary, ValueTypeBlob, ValueTypeClob:
		if col.TypeInfo.Precision >= 0 {
			return col.TypeInfo.Precision, true
		}
	}
	return 0, false
}

// ColumnTypePrecisionScale returns precision and scale for numeric types.
// This is part of the driver.RowsColumnTypePrecisionScale interface (optional).
func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	col := r.columns.GetColumn(index)
	if col == nil || col.TypeInfo == nil {
		return 0, 0, false
	}
	return col.TypeInfo.PrecisionScale()
}

// ExecuteQuery executes a query and returns the result set rows.
// This is a convenience method for simple query execution.
func (s *Session) ExecuteQuery(ctx context.Context, sql string, maxRows int64, fetchSize int) (*Rows, error) {
	if s.tr == nil {
		return nil, fmt.Errorf("h2go: ExecuteQuery: session closed")
	}

	cmd, err := s.PrepareCommand(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("h2go: ExecuteQuery: prepare failed: %w", err)
	}

	if !cmd.IsQuery {
		_ = cmd.Close(s)
		return nil, fmt.Errorf("h2go: ExecuteQuery: command is not a query: %s", sql)
	}

	return s.executeQueryWire(ctx, cmd, maxRows, fetchSize, nil, true)
}

// ExecuteQueryWithParams prepares a parameterised query with
// SESSION_PREPARE_READ_PARAMS2 (to obtain type metadata for encoding), executes
// it with the supplied positional values, and returns the result set.  Because
// this is an ad-hoc call the resulting Rows own the command and send
// COMMAND_CLOSE when the rows are closed.
func (s *Session) ExecuteQueryWithParams(ctx context.Context, sql string, maxRows int64, fetchSize int, params []driver.Value) (*Rows, error) {
	if s.tr == nil {
		return nil, fmt.Errorf("h2go: ExecuteQueryWithParams: session closed")
	}

	cmd, err := s.PrepareCommandReadParams(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("h2go: ExecuteQueryWithParams: prepare failed: %w", err)
	}

	if !cmd.IsQuery {
		_ = cmd.Close(s)
		return nil, fmt.Errorf("h2go: ExecuteQueryWithParams: command is not a query: %s", sql)
	}

	if int(cmd.ParamCount) != len(params) {
		_ = cmd.Close(s)
		return nil, fmt.Errorf("h2go: ExecuteQueryWithParams: expected %d params, got %d",
			cmd.ParamCount, len(params))
	}

	return s.executeQueryWire(ctx, cmd, maxRows, fetchSize, params, true)
}

// ExecuteQueryPrepared executes a prepared query command without parameters
// and returns the result set.
func (s *Session) ExecuteQueryPrepared(ctx context.Context, cmd *PreparedCommand, maxRows int64, fetchSize int) (*Rows, error) {
	return s.ExecuteQueryPreparedWithParams(ctx, cmd, maxRows, fetchSize, nil)
}

// ExecuteQueryPreparedWithParams executes a prepared query command with
// positional parameters and returns the result set.
// The command should have been prepared with PrepareCommandReadParams.
// The command is NOT closed when rows are closed - it can be reused.
func (s *Session) ExecuteQueryPreparedWithParams(ctx context.Context, cmd *PreparedCommand, maxRows int64, fetchSize int, params []driver.Value) (*Rows, error) {
	if s.tr == nil {
		return nil, fmt.Errorf("h2go: ExecuteQueryPreparedWithParams: session closed")
	}
	if cmd == nil {
		return nil, fmt.Errorf("h2go: ExecuteQueryPreparedWithParams: nil command")
	}
	if !cmd.IsQuery {
		return nil, fmt.Errorf("h2go: ExecuteQueryPreparedWithParams: command is not a query: %s", cmd.SQL)
	}
	if int(cmd.ParamCount) != len(params) {
		return nil, fmt.Errorf("h2go: ExecuteQueryPreparedWithParams: expected %d params, got %d",
			cmd.ParamCount, len(params))
	}

	return s.executeQueryWire(ctx, cmd, maxRows, fetchSize, params, false)
}

// executeQueryWire sends COMMAND_EXECUTE_QUERY for cmd and reads the initial
// response frame, returning open Rows.
//
// ownsCommand controls whether Rows.Close will also send COMMAND_CLOSE:
//   - true  for ad-hoc queries (ExecuteQuery, ExecuteQueryWithParams)
//   - false for reusable prepared statements (ExecuteQueryPreparedWithParams)
func (s *Session) executeQueryWire(ctx context.Context, cmd *PreparedCommand, maxRows int64, fetchSize int, params []driver.Value, ownsCommand bool) (*Rows, error) {
	resultID := s.nextCommandID()

	cmdIDForClose := int32(0)
	if ownsCommand {
		cmdIDForClose = cmd.ID
	}

	// Build the COMMAND_EXECUTE_QUERY request.
	if err := s.tr.WriteInt32(CommandExecuteQuery); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write op: %w", err)
	}
	if err := s.tr.WriteInt32(cmd.ID); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write command id: %w", err)
	}
	if err := s.tr.WriteInt32(resultID); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write result id: %w", err)
	}
	if err := s.tr.WriteInt64(maxRows); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write maxRows: %w", err)
	}
	if err := s.tr.WriteInt32(int32(fetchSize)); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write fetchSize: %w", err)
	}

	// sendParameters: writeInt(paramCount) followed by each value.
	if err := s.tr.WriteInt32(int32(len(params))); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to write paramCount: %w", err)
	}
	for i, p := range params {
		var paramType *TypeInfo
		if i < len(cmd.Params) {
			paramType = cmd.Params[i].TypeInfo
		}
		if err := s.tr.WriteValue(p, paramType); err != nil {
			if ownsCommand {
				_ = cmd.Close(s)
			}
			return nil, fmt.Errorf("h2go: executeQueryWire: encode param %d: %w", i+1, err)
		}
	}

	if err := s.tr.Flush(); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: flush failed: %w", err)
	}

	// Check context cancellation after flush (before blocking on response).
	select {
	case <-ctx.Done():
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, ctx.Err()
	default:
	}

	// Server responds: writeInt(status) . writeInt(columnCount) . writeRowCount . columns . firstBatch
	if err := readStatus(s.tr); err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, wrapError("executeQueryWire", err)
	}

	columnCount, err := s.tr.ReadInt32()
	if err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, fmt.Errorf("h2go: executeQueryWire: failed to read column count: %w", err)
	}

	rows, err := NewRows(s, resultID, columnCount, fetchSize, ownsCommand, cmdIDForClose)
	if err != nil {
		if ownsCommand {
			_ = cmd.Close(s)
		}
		return nil, err
	}
	return rows, nil
}
