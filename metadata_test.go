// metadata_test.go — Result set metadata reader tests.

package h2go

import (
	"bytes"
	"testing"
)

// TestReadResultMeta_SingleColumn tests reading metadata for a single column.
func TestReadResultMeta_SingleColumn(t *testing.T) {
	buf := new(bytes.Buffer)

	// Column: ID INTEGER NOT NULL
	writeString(buf, "ID")         // alias
	writeString(buf, "PUBLIC")     // schemaName
	writeString(buf, "USERS")      // tableName
	writeString(buf, "ID")         // columnName
	writeInt32(buf, TIInteger)     // type info: INTEGER
	writeByte(buf, 1)              // identity = true
	writeInt32(buf, ColumnNoNulls) // nullable

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(1, TCPProtocolVersion21)
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	if meta.ColumnCount != 1 {
		t.Errorf("ColumnCount: got %d, want 1", meta.ColumnCount)
	}
	if len(meta.Columns) != 1 {
		t.Fatalf("len(Columns): got %d, want 1", len(meta.Columns))
	}

	col := meta.Columns[0]
	if col.Alias != "ID" {
		t.Errorf("Alias: got %q, want ID", col.Alias)
	}
	if col.SchemaName != "PUBLIC" {
		t.Errorf("SchemaName: got %q, want PUBLIC", col.SchemaName)
	}
	if col.TableName != "USERS" {
		t.Errorf("TableName: got %q, want USERS", col.TableName)
	}
	if col.ColumnName != "ID" {
		t.Errorf("ColumnName: got %q, want ID", col.ColumnName)
	}
	if col.TypeInfo.ValueType != ValueTypeInteger {
		t.Errorf("TypeInfo.ValueType: got %d, want INTEGER", col.TypeInfo.ValueType)
	}
	if !col.Identity {
		t.Error("Identity: expected true")
	}
	if col.Nullable != ColumnNoNulls {
		t.Errorf("Nullable: got %d, want NO_NULLS", col.Nullable)
	}
}

// TestReadResultMeta_MultipleColumns tests reading metadata for multiple columns.
func TestReadResultMeta_MultipleColumns(t *testing.T) {
	buf := new(bytes.Buffer)

	// Column 1: ID INTEGER PRIMARY KEY
	writeString(buf, "ID")
	writeString(buf, "PUBLIC")
	writeString(buf, "TEST")
	writeString(buf, "ID")
	writeInt32(buf, TIInteger)
	writeByte(buf, 1) // identity
	writeInt32(buf, ColumnNoNulls)

	// Column 2: NAME VARCHAR(100) NULLABLE
	writeString(buf, "NAME")
	writeString(buf, "PUBLIC")
	writeString(buf, "TEST")
	writeString(buf, "NAME")
	writeInt32(buf, TIVarchar)
	writeInt32(buf, 100) // precision
	writeByte(buf, 0)    // not identity
	writeInt32(buf, ColumnNullable)

	// Column 3: SCORE DOUBLE
	writeString(buf, "SCORE")
	writeString(buf, "")
	writeString(buf, "") // no table
	writeString(buf, "")
	writeInt32(buf, TIDouble)
	writeByte(buf, 0xFF) // no precision
	writeByte(buf, 0)    // not identity
	writeInt32(buf, ColumnNullableUnknown)

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(3, TCPProtocolVersion21)
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	if meta.ColumnCount != 3 {
		t.Errorf("ColumnCount: got %d, want 3", meta.ColumnCount)
	}

	// Check column names
	names := meta.ColumnNames()
	expected := []string{"ID", "NAME", "SCORE"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("ColumnNames[%d]: got %q, want %q", i, names[i], want)
		}
	}
}

// TestReadResultMeta_NullableStrings tests handling of NULL schema/table/column names.
func TestReadResultMeta_NullableStrings(t *testing.T) {
	buf := new(bytes.Buffer)

	// Computed column with minimal metadata
	writeString(buf, "COUNT(*)") // alias
	writeNullString(buf)         // schemaName = null
	writeNullString(buf)         // tableName = null
	writeNullString(buf)         // columnName = null
	writeInt32(buf, TIBigint)
	writeByte(buf, 0) // not identity
	writeInt32(buf, ColumnNullableUnknown)

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(1, TCPProtocolVersion21)
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	col := meta.Columns[0]
	if col.Alias != "COUNT(*)" {
		t.Errorf("Alias: got %q, want COUNT(*)", col.Alias)
	}
	if col.SchemaName != "" {
		t.Errorf("SchemaName: got %q, want empty", col.SchemaName)
	}
	if col.TableName != "" {
		t.Errorf("TableName: got %q, want empty", col.TableName)
	}
	if col.ColumnName != "" {
		t.Errorf("ColumnName: got %q, want empty", col.ColumnName)
	}
}

// TestReadResultMeta_Protocol19_DisplaySize tests protocol 19 displaySize skip.
func TestReadResultMeta_Protocol19_DisplaySize(t *testing.T) {
	buf := new(bytes.Buffer)

	// Same column, but with displaySize field for protocol < 20
	writeString(buf, "ID")
	writeString(buf, "PUBLIC")
	writeString(buf, "TEST")
	writeString(buf, "ID")
	writeInt32(buf, TIInteger)
	writeInt32(buf, 11) // displaySize (skipped for protocol 19)
	writeByte(buf, 0)   // not identity
	writeInt32(buf, ColumnNoNulls)

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(1, TCPProtocolVersion20-1) // protocol 19
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	if len(meta.Columns) != 1 {
		t.Fatalf("len(Columns): got %d, want 1", len(meta.Columns))
	}

	col := meta.Columns[0]
	if col.Alias != "ID" {
		t.Errorf("Alias: got %q, want ID", col.Alias)
	}
}

// TestResultMeta_ColumnNames tests extracting column names.
func TestResultMeta_ColumnNames(t *testing.T) {
	meta := &ResultMeta{
		ColumnCount: 3,
		Columns: []ResultColumn{
			{Alias: "col_a"},
			{Alias: "col_b"},
			{Alias: "col_c"},
		},
	}

	names := meta.ColumnNames()
	if len(names) != 3 {
		t.Fatalf("len(names): got %d, want 3", len(names))
	}

	want := []string{"col_a", "col_b", "col_c"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d]: got %q, want %q", i, names[i], w)
		}
	}
}

// TestResultMeta_GetColumn tests retrieving a column by index.
func TestResultMeta_GetColumn(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "first", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
			{Alias: "second", TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar}},
		},
	}

	col := meta.GetColumn(0)
	if col == nil {
		t.Fatal("GetColumn(0): got nil")
	}
	if col.Alias != "first" {
		t.Errorf("GetColumn(0).Alias: got %q, want first", col.Alias)
	}

	col = meta.GetColumn(1)
	if col == nil {
		t.Fatal("GetColumn(1): got nil")
	}
	if col.Alias != "second" {
		t.Errorf("GetColumn(1).Alias: got %q, want second", col.Alias)
	}

	// Out of range
	col = meta.GetColumn(2)
	if col != nil {
		t.Error("GetColumn(2): expected nil for out of range")
	}

	col = meta.GetColumn(-1)
	if col != nil {
		t.Error("GetColumn(-1): expected nil for negative index")
	}
}

// TestResultMeta_GetColumnByName tests retrieving a column by alias.
func TestResultMeta_GetColumnByName(t *testing.T) {
	meta := &ResultMeta{
		Columns: []ResultColumn{
			{Alias: "id", TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
			{Alias: "name", TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar}},
			{Alias: "active", TypeInfo: &TypeInfo{ValueType: ValueTypeBoolean}},
		},
	}

	col := meta.GetColumnByName("name")
	if col == nil {
		t.Fatal("GetColumnByName(\"name\"): got nil")
	}
	if col.TypeInfo.ValueType != ValueTypeVarchar {
		t.Errorf("Type: got %d, want VARCHAR", col.TypeInfo.ValueType)
	}

	// Not found
	col = meta.GetColumnByName("nonexistent")
	if col != nil {
		t.Error("GetColumnByName(\"nonexistent\"): expected nil")
	}
}

// TestNullableString tests the nullable constant string representation.
func TestNullableString(t *testing.T) {
	tests := []struct {
		value int
		want  string
	}{
		{ColumnNoNulls, "NO_NULLS"},
		{ColumnNullable, "NULLABLE"},
		{ColumnNullableUnknown, "NULLABLE_UNKNOWN"},
		{99, "UNKNOWN(99)"},
	}

	for _, tc := range tests {
		got := NullableString(tc.value)
		if got != tc.want {
			t.Errorf("NullableString(%d): got %q, want %q", tc.value, got, tc.want)
		}
	}
}

// TestResultColumn_WithTypeInfo tests ResultColumn holding TypeInfo.
func TestResultColumn_WithTypeInfo(t *testing.T) {
	col := ResultColumn{
		Alias:      "price",
		SchemaName: "PUBLIC",
		TableName:  "PRODUCTS",
		ColumnName: "PRICE",
		TypeInfo: &TypeInfo{
			ValueType: ValueTypeNumeric,
			Precision: 10,
			Scale:     2,
		},
		Identity: false,
		Nullable: ColumnNullable,
	}

	if col.TypeInfo.ValueType != ValueTypeNumeric {
		t.Errorf("TypeInfo.ValueType: got %d, want NUMERIC", col.TypeInfo.ValueType)
	}
	if col.TypeInfo.Precision != 10 {
		t.Errorf("TypeInfo.Precision: got %d, want 10", col.TypeInfo.Precision)
	}
	if col.TypeInfo.Scale != 2 {
		t.Errorf("TypeInfo.Scale: got %d, want 2", col.TypeInfo.Scale)
	}
}

// TestReadResultMeta_NumericColumn tests reading a NUMERIC column metadata.
func TestReadResultMeta_NumericColumn(t *testing.T) {
	buf := new(bytes.Buffer)

	// PRICE DECIMAL(10,2)
	writeString(buf, "PRICE")
	writeString(buf, "PUBLIC")
	writeString(buf, "PRODUCTS")
	writeString(buf, "PRICE")
	writeInt32(buf, TINumeric)
	writeInt32(buf, 10) // precision
	writeInt32(buf, 2)  // scale
	writeByte(buf, 0)   // hasExt = false
	writeByte(buf, 0)   // not identity
	writeInt32(buf, ColumnNullable)

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(1, TCPProtocolVersion21)
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	col := meta.Columns[0]
	if col.TypeInfo.ValueType != ValueTypeNumeric {
		t.Errorf("ValueType: got %d, want NUMERIC", col.TypeInfo.ValueType)
	}
	if col.TypeInfo.Precision != 10 {
		t.Errorf("Precision: got %d, want 10", col.TypeInfo.Precision)
	}
	if col.TypeInfo.Scale != 2 {
		t.Errorf("Scale: got %d, want 2", col.TypeInfo.Scale)
	}
}

// TestReadResultMeta_TimestampColumn tests reading a TIMESTAMP column.
func TestReadResultMeta_TimestampColumn(t *testing.T) {
	buf := new(bytes.Buffer)

	writeString(buf, "CREATED_AT")
	writeString(buf, "PUBLIC")
	writeString(buf, "EVENTS")
	writeString(buf, "CREATED_AT")
	writeInt32(buf, TITimestamp)
	writeByte(buf, 9) // scale (nanoseconds precision)
	writeByte(buf, 0) // not identity
	writeInt32(buf, ColumnNullable)

	tr := mockTransferFromBytes(buf.Bytes())
	meta, err := tr.ReadResultMeta(1, TCPProtocolVersion21)
	if err != nil {
		t.Fatalf("ReadResultMeta failed: %v", err)
	}

	col := meta.Columns[0]
	if col.TypeInfo.ValueType != ValueTypeTimestamp {
		t.Errorf("ValueType: got %d, want TIMESTAMP", col.TypeInfo.ValueType)
	}
	if col.TypeInfo.Scale != 9 {
		t.Errorf("Scale: got %d, want 9", col.TypeInfo.Scale)
	}
}

// Helper functions

func writeString(buf *bytes.Buffer, s string) {
	// H2 string encoding: int32 length in UTF-16 code units, then UTF-16 BE
	// For testing, we use simple ASCII-only strings
	runes := []rune(s)
	writeInt32(buf, int32(len(runes)))
	for _, r := range runes {
		// Write as big-endian uint16
		buf.WriteByte(byte(r >> 8))
		buf.WriteByte(byte(r))
	}
}

func writeNullString(buf *bytes.Buffer) {
	// Null string: length = -1
	writeInt32(buf, -1)
}
