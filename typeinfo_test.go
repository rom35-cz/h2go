// typeinfo_test.go — TypeInfo protocol-21 decoder tests.

package h2go

import (
	"bytes"
	"testing"
)

// mockTransfer creates a Tr that reads from the provided bytes.
func mockTransfer(data []byte) *Tr {
	return NewReader(bytes.NewReader(data))
}

// TestReadTypeInfo_Null tests reading a NULL type info.
func TestReadTypeInfo_Null(t *testing.T) {
	// TI_NULL = 0, no additional fields
	data := writeInt32BE(TINull)
	tr := mockTransfer(data)

	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeNull {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeNull)
	}
	if info.Precision != -1 {
		t.Errorf("Precision: got %d, want -1", info.Precision)
	}
	if info.Scale != -1 {
		t.Errorf("Scale: got %d, want -1", info.Scale)
	}
}

// TestReadTypeInfo_Boolean tests reading a BOOLEAN type info.
func TestReadTypeInfo_Boolean(t *testing.T) {
	data := writeInt32BE(TIBoolean)
	tr := mockTransfer(data)

	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeBoolean {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeBoolean)
	}
}

// TestReadTypeInfo_Integer tests reading an INTEGER type info.
func TestReadTypeInfo_Integer(t *testing.T) {
	data := writeInt32BE(TIInteger)
	tr := mockTransfer(data)

	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeInteger {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeInteger)
	}
}

// TestReadTypeInfo_Bigint tests reading a BIGINT type info.
func TestReadTypeInfo_Bigint(t *testing.T) {
	data := writeInt32BE(TIBigint)
	tr := mockTransfer(data)

	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeBigint {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeBigint)
	}
}

// TestReadTypeInfo_Varchar tests reading a VARCHAR type info with precision.
func TestReadTypeInfo_Varchar(t *testing.T) {
	// TI_VARCHAR = 13, precision as int32
	buf := new(bytes.Buffer)
	writeInt32(buf, TIVarchar)
	writeInt32(buf, 200) // precision

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeVarchar {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeVarchar)
	}
	if info.Precision != 200 {
		t.Errorf("Precision: got %d, want 200", info.Precision)
	}
}

// TestReadTypeInfo_Char tests reading a CHAR type info with precision.
func TestReadTypeInfo_Char(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIChar)
	writeInt32(buf, 10) // precision (length)

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeChar {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeChar)
	}
	if info.Precision != 10 {
		t.Errorf("Precision: got %d, want 10", info.Precision)
	}
}

// TestReadTypeInfo_Numeric tests reading a NUMERIC type info with precision and scale.
func TestReadTypeInfo_Numeric(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TINumeric)
	writeInt32(buf, 15) // precision
	writeInt32(buf, 5)  // scale
	writeByte(buf, 0)   // hasExt = false

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeNumeric {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeNumeric)
	}
	if info.Precision != 15 {
		t.Errorf("Precision: got %d, want 15", info.Precision)
	}
	if info.Scale != 5 {
		t.Errorf("Scale: got %d, want 5", info.Scale)
	}
}

// TestReadTypeInfo_Double tests reading a DOUBLE type info with precision byte.
func TestReadTypeInfo_Double(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIDouble)
	writeByte(buf, 0xFF) // -1 as unsigned = no explicit precision

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeDouble {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeDouble)
	}
	// Precision should remain -1 when byte is 0xFF
	if info.Precision != -1 {
		t.Errorf("Precision: got %d, want -1", info.Precision)
	}
}

// TestReadTypeInfo_Timestamp tests reading a TIMESTAMP type info with scale byte.
func TestReadTypeInfo_Timestamp(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TITimestamp)
	writeByte(buf, 9) // scale (nanoseconds precision)

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeTimestamp {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeTimestamp)
	}
	if info.Scale != 9 {
		t.Errorf("Scale: got %d, want 9", info.Scale)
	}
}

// TestReadTypeInfo_TimestampTZ tests reading a TIMESTAMP WITH TIME ZONE type info.
func TestReadTypeInfo_TimestampTZ(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TITimestampTZ)
	writeByte(buf, 6) // scale

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeTimestampTZ {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeTimestampTZ)
	}
}

// TestReadTypeInfo_Blob tests reading a BLOB type info with long precision.
func TestReadTypeInfo_Blob(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIBlob)
	writeInt64(buf, 1048576) // precision (1 MB)

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeBlob {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeBlob)
	}
	if info.Precision != 1048576 {
		t.Errorf("Precision: got %d, want 1048576", info.Precision)
	}
}

// TestReadTypeInfo_Decfloat tests reading a DECFLOAT type info.
func TestReadTypeInfo_Decfloat(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIDecfloat)
	writeInt32(buf, 34) // precision

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	if info.ValueType != ValueTypeDecfloat {
		t.Errorf("ValueType: got %d, want %d", info.ValueType, ValueTypeDecfloat)
	}
	if info.Precision != 34 {
		t.Errorf("Precision: got %d, want 34", info.Precision)
	}
}

// TestReadTypeInfo_UnknownTypeCode tests handling of unknown type codes.
func TestReadTypeInfo_UnknownTypeCode(t *testing.T) {
	// Use a very high type code that doesn't exist
	buf := new(bytes.Buffer)
	writeInt32(buf, 999)

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo failed: %v", err)
	}

	// Should map to NULL/UNKNOWN
	if info.ValueType != ValueTypeNull {
		t.Errorf("ValueType: got %d, want %d (NULL/UNKNOWN)", info.ValueType, ValueTypeNull)
	}
}

// TestTypeInfo_DatabaseTypeName tests type name strings.
func TestTypeInfo_DatabaseTypeName(t *testing.T) {
	tests := []struct {
		valueType int
		want      string
	}{
		{ValueTypeNull, "NULL"},
		{ValueTypeBoolean, "BOOLEAN"},
		{ValueTypeTinyint, "TINYINT"},
		{ValueTypeSmallint, "SMALLINT"},
		{ValueTypeInteger, "INTEGER"},
		{ValueTypeBigint, "BIGINT"},
		{ValueTypeNumeric, "NUMERIC"},
		{ValueTypeDouble, "DOUBLE"},
		{ValueTypeReal, "REAL"},
		{ValueTypeTime, "TIME"},
		{ValueTypeDate, "DATE"},
		{ValueTypeTimestamp, "TIMESTAMP"},
		{ValueTypeTimestampTZ, "TIMESTAMP WITH TIME ZONE"},
		{ValueTypeTimeTZ, "TIME WITH TIME ZONE"},
		{ValueTypeVarchar, "VARCHAR"},
		{ValueTypeChar, "CHAR"},
		{ValueTypeBlob, "BLOB"},
		{ValueTypeClob, "CLOB"},
		{ValueTypeUUID, "UUID"},
		{ValueTypeJSON, "JSON"},
		{ValueTypeDecfloat, "DECFLOAT"},
		{999, "UNKNOWN"}, // unknown type
	}

	for _, tc := range tests {
		info := &TypeInfo{ValueType: tc.valueType}
		got := info.DatabaseTypeName()
		if got != tc.want {
			t.Errorf("DatabaseTypeName(ValueType=%d): got %q, want %q", tc.valueType, got, tc.want)
		}
	}
}

// TestTypeInfo_HasPrecisionScale tests the precision/scale detection.
func TestTypeInfo_HasPrecisionScale(t *testing.T) {
	tests := []struct {
		valueType    int
		hasPrecision bool
	}{
		{ValueTypeNumeric, true},
		{ValueTypeDecfloat, true},
		{ValueTypeInteger, false},
		{ValueTypeVarchar, false},
	}

	for _, tc := range tests {
		info := &TypeInfo{ValueType: tc.valueType, Precision: 10, Scale: 2}
		got := info.HasPrecisionScale()
		if got != tc.hasPrecision {
			t.Errorf("HasPrecisionScale(ValueType=%d): got %v, want %v", tc.valueType, got, tc.hasPrecision)
		}
	}
}

// TestTypeInfo_PrecisionScale tests returning precision and scale.
func TestTypeInfo_PrecisionScale(t *testing.T) {
	// NUMERIC with precision/scale
	info := &TypeInfo{ValueType: ValueTypeNumeric, Precision: 15, Scale: 5}
	prec, scale, ok := info.PrecisionScale()
	if !ok {
		t.Error("Expected ok=true for NUMERIC")
	}
	if prec != 15 || scale != 5 {
		t.Errorf("PrecisionScale: got (%d, %d), want (15, 5)", prec, scale)
	}

	// INTEGER - no precision/scale
	info = &TypeInfo{ValueType: ValueTypeInteger, Precision: 10, Scale: 0}
	_, _, ok = info.PrecisionScale()
	if ok {
		t.Error("Expected ok=false for INTEGER")
	}
}

// Helper functions for building test data

func writeInt32BE(v int32) []byte {
	buf := new(bytes.Buffer)
	writeInt32(buf, v)
	return buf.Bytes()
}

func writeInt32(buf *bytes.Buffer, v int32) {
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
}

func writeInt64(buf *bytes.Buffer, v int64) {
	for i := 7; i >= 0; i-- {
		buf.WriteByte(byte(v >> (i * 8)))
	}
}

func writeByte(buf *bytes.Buffer, v byte) {
	buf.WriteByte(v)
}
