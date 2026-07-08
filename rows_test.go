// rows_test.go — driver.Rows implementation tests.

package h2go

import (
	"bytes"
	"context"
	"database/sql/driver"
	"io"
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

// TestRows_Closed tests behavior on closed rows.
func TestRows_Closed(t *testing.T) {
	r := &Rows{closed: true, columnCount: 2}

	err := r.Next(make([]driver.Value, 2))
	if err != driver.ErrBadConn {
		t.Errorf("Next on closed: got %v, want ErrBadConn", err)
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

// Helper type for nopCloser
type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }
