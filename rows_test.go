// rows_test.go — driver.Rows implementation tests.

package h2go

import (
	"bytes"
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"net"
	"testing"
)

// TestRows_Columns tests the Columns() method.
func TestRows_Columns(t *testing.T) {
	meta := &ResultMeta{
		ColumnCount: 3,
		Columns: []ResultColumn{
			{Alias: "id"},
			{Alias: "name"},
			{Alias: "email"},
		},
	}

	r := &Rows{columns: meta, columnCount: 3}
	cols := r.Columns()

	if len(cols) != 3 {
		t.Fatalf("len(cols): got %d, want 3", len(cols))
	}

	want := []string{"id", "name", "email"}
	for i, w := range want {
		if cols[i] != w {
			t.Errorf("cols[%d]: got %q, want %q", i, cols[i], w)
		}
	}
}

// TestRows_ColumnTypeDatabaseTypeName tests type name retrieval.
func TestRows_ColumnTypeDatabaseTypeName(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "id", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
			{Alias: "name", TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar}},
		},
	}

	r := &Rows{columns: meta}

	if got := r.ColumnTypeDatabaseTypeName(0); got != "INTEGER" {
		t.Errorf("ColumnTypeDatabaseTypeName(0): got %q, want INTEGER", got)
	}
	if got := r.ColumnTypeDatabaseTypeName(1); got != "VARCHAR" {
		t.Errorf("ColumnTypeDatabaseTypeName(1): got %q, want VARCHAR", got)
	}
}

// TestRows_ColumnTypeNullable tests nullable detection.
func TestRows_ColumnTypeNullable(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "id", Nullable: ColumnNoNulls},
			{Alias: "name", Nullable: ColumnNullable},
			{Alias: "desc", Nullable: ColumnNullableUnknown},
		},
	}

	r := &Rows{columns: meta}

	tests := []struct {
		index    int
		wantNull bool
		wantOK   bool
	}{
		{0, false, true},  // NO_NULLS
		{1, true, true},   // NULLABLE
		{2, false, false}, // UNKNOWN
	}

	for _, tc := range tests {
		nullable, ok := r.ColumnTypeNullable(tc.index)
		if ok != tc.wantOK {
			t.Errorf("ColumnTypeNullable(%d): ok=%v, want %v", tc.index, ok, tc.wantOK)
		}
		if ok && nullable != tc.wantNull {
			t.Errorf("ColumnTypeNullable(%d): nullable=%v, want %v", tc.index, nullable, tc.wantNull)
		}
	}
}

// TestRows_ColumnTypeLength tests length detection for variable-length types.
func TestRows_ColumnTypeLength(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "name", TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar, Precision: 100}},
			{Alias: "data", TypeInfo: &TypeInfo{ValueType: ValueTypeBlob, Precision: 1048576}},
			{Alias: "id", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
		},
	}

	r := &Rows{columns: meta}

	// VARCHAR should have length
	length, ok := r.ColumnTypeLength(0)
	if !ok {
		t.Error("ColumnTypeLength(0): expected ok=true for VARCHAR")
	}
	if length != 100 {
		t.Errorf("ColumnTypeLength(0): length=%d, want 100", length)
	}

	// BLOB should have length
	length, ok = r.ColumnTypeLength(1)
	if !ok {
		t.Error("ColumnTypeLength(1): expected ok=true for BLOB")
	}
	if length != 1048576 {
		t.Errorf("ColumnTypeLength(1): length=%d, want 1048576", length)
	}

	// INTEGER should not have length
	_, ok = r.ColumnTypeLength(2)
	if ok {
		t.Error("ColumnTypeLength(2): expected ok=false for INTEGER")
	}
}

// TestRows_ColumnTypePrecisionScale tests precision/scale for numeric types.
func TestRows_ColumnTypePrecisionScale(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "price", TypeInfo: &TypeInfo{ValueType: ValueTypeNumeric, Precision: 10, Scale: 2}},
			{Alias: "count", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
		},
	}

	r := &Rows{columns: meta}

	// NUMERIC should have precision/scale
	prec, scale, ok := r.ColumnTypePrecisionScale(0)
	if !ok {
		t.Error("ColumnTypePrecisionScale(0): expected ok=true for NUMERIC")
	}
	if prec != 10 || scale != 2 {
		t.Errorf("ColumnTypePrecisionScale(0): prec=%d, scale=%d, want 10, 2", prec, scale)
	}

	// INTEGER should not have precision/scale
	_, _, ok = r.ColumnTypePrecisionScale(1)
	if ok {
		t.Error("ColumnTypePrecisionScale(1): expected ok=false for INTEGER")
	}
}

// TestRows_NextResultSet tests that multiple result sets are not supported.
func TestRows_NextResultSet(t *testing.T) {
	r := &Rows{}

	if r.HasNextResultSet() {
		t.Error("HasNextResultSet: expected false")
	}

	err := r.NextResultSet()
	if err == nil {
		t.Error("NextResultSet: expected error")
	}
}

// TestRows_Closed tests behavior on closed rows. Next on a closed result set
// must report io.EOF (no more rows), not driver.ErrBadConn, which would
// wrongly mark the underlying connection as dead.
func TestRows_Closed(t *testing.T) {
	r := &Rows{closed: true, columnCount: 2}

	err := r.Next(make([]driver.Value, 2))
	if err != io.EOF {
		t.Errorf("Next on closed: got %v, want io.EOF", err)
	}
}

// TestRows_Next_WrongDestLength tests error on mismatched dest length.
func TestRows_Next_WrongDestLength(t *testing.T) {
	r := &Rows{columnCount: 3, columns: &ResultMeta{ColumnCount: 3}}

	err := r.Next(make([]driver.Value, 2))
	if err == nil {
		t.Fatal("Expected error for wrong dest length")
	}
}

// TestRows_Next_StickyError verifies that once an error is recorded on the
// Rows (e.g. a mid-stream fetch failure), subsequent Next calls return that
// same error instead of attempting another RESULT_FETCH_ROWS on a possibly
// misaligned read stream.
func TestRows_Next_StickyError(t *testing.T) {
	sentinel := fmt.Errorf("h2go: mock fetch failure")
	r := &Rows{
		columnCount: 2,
		columns:     &ResultMeta{ColumnCount: 2},
		rowCount:    -1, // lazy: would normally trigger a fetch
		err:         sentinel,
	}

	if err := r.Next(make([]driver.Value, 2)); err != sentinel {
		t.Errorf("Next with sticky err: got %v, want sentinel", err)
	}
}

// TestRows_Next_EOF tests EOF when no more rows.
func TestRows_Next_EOF(t *testing.T) {
	// No buffered rows and rowCount reached
	r := &Rows{
		columnCount:  2,
		columns:      &ResultMeta{ColumnCount: 2},
		rowCount:     0,
		rowOffset:    0,
		bufferedRows: nil,
	}

	dest := make([]driver.Value, 2)
	err := r.Next(dest)
	if err != io.EOF {
		t.Errorf("Next: got %v, want io.EOF", err)
	}
}

// TestRows_WithBufferedRow tests reading a buffered row.
func TestRows_WithBufferedRow(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "id", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
			{Alias: "name", TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar}},
		},
	}

	r := &Rows{
		columnCount:  2,
		columns:      meta,
		bufferedRows: [][]driver.Value{{int64(1), "alice"}},
		rowCount:     1,
	}

	dest := make([]driver.Value, 2)
	err := r.Next(dest)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}

	if dest[0] != int64(1) {
		t.Errorf("dest[0]: got %v, want 1", dest[0])
	}
	if dest[1] != "alice" {
		t.Errorf("dest[1]: got %v, want alice", dest[1])
	}

	// Second Next should return EOF
	err = r.Next(dest)
	if err != io.EOF {
		t.Errorf("Second Next: got %v, want io.EOF", err)
	}
}

// TestRows_Close_Idempotent tests that Close is idempotent.
func TestRows_Close_Idempotent(t *testing.T) {
	r := &Rows{closed: false}

	// First close
	if err := r.Close(); err != nil {
		t.Errorf("First Close: %v", err)
	}

	// Second close should not error
	if err := r.Close(); err != nil {
		t.Errorf("Second Close: %v", err)
	}

	if !r.closed {
		t.Error("Expected closed to be true")
	}
}

// TestExecuteQuery_NilSession tests error on nil session.
func TestExecuteQuery_NilSession(t *testing.T) {
	var s *Session
	// Since ExecuteQuery is a method on *Session, calling it on nil panics
	// before we can check. This is expected behavior - callers must not
	// call methods on nil sessions.
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil session")
		}
	}()
	_, _ = s.ExecuteQuery(context.TODO(), "SELECT 1", 0, 100)
}

// TestExecuteQueryPrepared_NotQuery tests error when command is not a query.
func TestExecuteQueryPrepared_NotQuery(t *testing.T) {
	// Create a mock session with nil transport (will fail later, but tests the check)
	s := &Session{}
	cmd := &PreparedCommand{IsQuery: false, SQL: "INSERT INTO t VALUES (1)"}

	_, err := s.ExecuteQueryPrepared(context.TODO(), cmd, 0, 100)
	if err == nil {
		t.Fatal("Expected error for non-query command")
	}
}

// TestExecuteQueryPrepared_ReadsStatus verifies that ExecuteQueryPrepared
// correctly reads the STATUS int before the column count.
// Regression test for bug where readStatus was missing, causing the
// status int (1 = STATUS_OK) to be consumed as columnCount.
// For a 2-column query, STATUS_OK=1 would produce columnCount=1 (wrong).
func TestExecuteQueryPrepared_ReadsStatus(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		tr := NewReadWriter(serverConn)
		// Drain the COMMAND_EXECUTE_QUERY request.
		_, _ = tr.ReadInt32() // op
		_, _ = tr.ReadInt32() // command id
		_, _ = tr.ReadInt32() // result id
		_, _ = tr.ReadInt64() // maxRows
		_, _ = tr.ReadInt32() // fetchSize
		_, _ = tr.ReadInt32() // paramCount

		// Write response: STATUS_OK + columnCount=2 + rowCount=0 + 2 column metadata + no rows
		_ = tr.WriteInt32(StatusOK) // status
		_ = tr.WriteInt32(2)        // columnCount = 2 (catches the bug: if status consumed as count, we'd get 1)
		_ = tr.WriteRowCount(0)     // rowCount

		// Column 1: alias="a", schema="", table="", name="a", typeinfo=INTEGER, identity=false, nullable=2
		for _, alias := range []string{"a", "b"} {
			_ = tr.WriteString(alias)
			_ = tr.WriteNullString() // schema
			_ = tr.WriteNullString() // table
			_ = tr.WriteNullString() // column name
			// TypeInfo: TIInteger + no extra bytes
			_ = tr.WriteInt32(TIInteger)
			_ = tr.WriteBool(false) // identity
			_ = tr.WriteInt32(2)    // nullable
		}

		// End of rows flag
		_ = tr.WriteByte(0) // flag=0 = end of result
		if err := tr.Flush(); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
		// Drain the RESULT_CLOSE that rows.Close() sends (no server response expected).
		_, _ = tr.ReadInt32() // op = ResultClose
		_, _ = tr.ReadInt32() // result id
	}()

	sess := &Session{tr: NewReadWriter(clientConn), id: "test", version: 21}
	cmd := &PreparedCommand{
		ID: 1, SQL: "SELECT a, b FROM t",
		IsQuery: true, ParamCount: 0,
	}
	rows, err := sess.ExecuteQueryPrepared(context.Background(), cmd, 0, 100)
	if err != nil {
		<-errCh
		t.Fatalf("ExecuteQueryPrepared failed: %v", err)
	}
	<-errCh // wait for server goroutine to finish writing

	if int(rows.columnCount) != 2 {
		t.Errorf("columnCount: got %d, want 2", rows.columnCount)
	}
	cols := rows.Columns()
	if len(cols) != 2 || cols[0] != "a" || cols[1] != "b" {
		t.Errorf("Columns: got %v, want [a b]", cols)
	}
	// Close rows — server goroutine drains RESULT_CLOSE.
	_ = rows.Close()
}

// TestRows_NoMoreRows_PreventsExtraFetch verifies that after receiving the
// end-of-result flag (byte 0) in fetchRows, the noMoreRows guard prevents
// sending an extra RESULT_FETCH_ROWS request to the server.
func TestRows_NoMoreRows_PreventsExtraFetch(t *testing.T) {
	// Simulate rows that had all data in the initial batch (flag=0 already seen).
	r := &Rows{
		columnCount:  2,
		columns:      &ResultMeta{Columns: []ResultColumn{{Alias: "x"}, {Alias: "y"}}},
		rowCount:     -1, // lazy/unknown — no row-count EOF guard
		bufferedRows: nil,
		noMoreRows:   true, // flag=0 was already received
	}

	dest := make([]driver.Value, 2)
	err := r.Next(dest)
	if err != io.EOF {
		t.Errorf("Next after noMoreRows: got %v, want io.EOF", err)
	}
}

// Helper type for nopCloser
type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }
