// typeinfo_test.go — TypeInfo protocol-21 decoder tests.

package h2go

import (
	"bytes"
	"reflect"
	"testing"
	"time"
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

// TestReadTypeInfo_IntervalYear_OneByte verifies that INTERVAL YEAR TypeInfo
// reads exactly 1 precision byte (no trailing scale byte).
// Confirmed by H2 Transfer.java writeTypeInfo20: non-fractional-second interval
// types write only writeBytePrecisionWithDefault.
func TestReadTypeInfo_IntervalYear_OneByte(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIIntervalYear) // TI code 26
	writeByte(buf, 2)               // precision byte
	// Do NOT write a scale byte — only 1 byte is expected.

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo INTERVAL YEAR failed: %v", err)
	}
	if info.ValueType != ValueTypeInterval {
		t.Errorf("ValueType: got %d, want ValueTypeInterval", info.ValueType)
	}
	if info.Precision != 2 {
		t.Errorf("Precision: got %d, want 2", info.Precision)
	}
	if info.Scale != -1 {
		t.Errorf("Scale: got %d, want -1 (no scale for INTERVAL YEAR)", info.Scale)
	}
}

// TestReadTypeInfo_IntervalSecond_TwoBytes verifies that INTERVAL SECOND
// TypeInfo reads both a precision byte and a scale byte.
func TestReadTypeInfo_IntervalSecond_TwoBytes(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, TIIntervalSecond) // TI code 31
	writeByte(buf, 4)                 // precision byte
	writeByte(buf, 6)                 // scale byte

	tr := mockTransfer(buf.Bytes())
	info, err := tr.ReadTypeInfo()
	if err != nil {
		t.Fatalf("ReadTypeInfo INTERVAL SECOND failed: %v", err)
	}
	if info.ValueType != ValueTypeInterval {
		t.Errorf("ValueType: got %d, want ValueTypeInterval", info.ValueType)
	}
	if info.Precision != 4 {
		t.Errorf("Precision: got %d, want 4", info.Precision)
	}
	if info.Scale != 6 {
		t.Errorf("Scale: got %d, want 6", info.Scale)
	}
}

// TestTIConstants_MatchH2 verifies critical TI code constants against their
// known H2 2.4.240 values from Transfer.java.
func TestTIConstants_MatchH2(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"TINull", TINull, 0},
		{"TIBoolean", TIBoolean, 1},
		{"TIVarchar", TIVarchar, 13},
		{"TIJavaObject", TIJavaObject, 19},
		{"TIUUID", TIUUID, 20},
		{"TIChar", TIChar, 21},
		{"TIGeometry", TIGeometry, 22},
		// 23 is unused in H2
		{"TITimestampTZ", TITimestampTZ, 24},
		{"TIEnum", TIEnum, 25},
		{"TIIntervalYear", TIIntervalYear, 26},
		{"TIIntervalSecond", TIIntervalSecond, 31},
		{"TIIntervalDaySecond", TIIntervalDaySecond, 35},
		{"TIIntervalHourSec", TIIntervalHourSec, 37},
		{"TIIntervalMinSec", TIIntervalMinSec, 38},
		{"TIRow", TIRow, 39},
		{"TIJSON", TIJSON, 40},
		{"TITimeTZ", TITimeTZ, 41},
		{"TIBinary", TIBinary, 42},
		{"TIDecfloat", TIDecfloat, 43},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
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
		{ValueTypeTimestamp, true},
		{ValueTypeTimestampTZ, true},
		{ValueTypeTime, true},
		{ValueTypeTimeTZ, true},
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
	// NUMERIC with known precision/scale
	info := &TypeInfo{ValueType: ValueTypeNumeric, Precision: 15, Scale: 5}
	prec, scale, ok := info.PrecisionScale()
	if !ok {
		t.Error("Expected ok=true for NUMERIC(15,5)")
	}
	if prec != 15 || scale != 5 {
		t.Errorf("PrecisionScale: got (%d, %d), want (15, 5)", prec, scale)
	}

	// NUMERIC with unknown precision (manually constructed — e.g. from tests or
	// future wire formats that omit precision).
	info = &TypeInfo{ValueType: ValueTypeNumeric, Precision: -1, Scale: -1}
	_, _, ok = info.PrecisionScale()
	if ok {
		t.Error("Expected ok=false for NUMERIC with unknown precision")
	}

	// NUMERIC(12) declared without scale: wire sends scale=-1, which must be
	// reported as unknown (ok=false) rather than falsely claiming scale 0.
	info = &TypeInfo{ValueType: ValueTypeNumeric, Precision: 12, Scale: -1}
	prec, scale, ok = info.PrecisionScale()
	if ok {
		t.Error("Expected ok=false for scale-less NUMERIC(12)")
	}
	if prec != 12 || scale != 0 {
		t.Errorf("PrecisionScale scale-less NUMERIC: got (%d, %d), want (12, 0)", prec, scale)
	}

	// NUMERIC(12,4) with explicit scale stays known.
	info = &TypeInfo{ValueType: ValueTypeNumeric, Precision: 12, Scale: 4}
	prec, scale, ok = info.PrecisionScale()
	if !ok {
		t.Error("Expected ok=true for NUMERIC(12,4)")
	}
	if prec != 12 || scale != 4 {
		t.Errorf("PrecisionScale NUMERIC(12,4): got (%d, %d), want (12, 4)", prec, scale)
	}

	// DECFLOAT reports precision but no fixed scale.
	info = &TypeInfo{ValueType: ValueTypeDecfloat, Precision: 34, Scale: -1}
	prec, scale, ok = info.PrecisionScale()
	if !ok {
		t.Error("Expected ok=true for DECFLOAT(34)")
	}
	if prec != 34 || scale != 0 {
		t.Errorf("PrecisionScale DECFLOAT: got (%d, %d), want (34, 0)", prec, scale)
	}

	// TIMESTAMP WITH TIME ZONE — explicit scale.
	info = &TypeInfo{ValueType: ValueTypeTimestampTZ, Precision: -1, Scale: 6}
	prec, scale, ok = info.PrecisionScale()
	if !ok {
		t.Error("Expected ok=true for TIMESTAMP WITH TIME ZONE (scale=6)")
	}
	if prec != 0 || scale != 6 {
		t.Errorf("PrecisionScale: got (%d, %d), want (0, 6)", prec, scale)
	}

	// TIMESTAMP without explicit scale (wire sends 0xFF → Scale=-1).
	info = &TypeInfo{ValueType: ValueTypeTimestamp, Precision: -1, Scale: -1}
	_, _, ok = info.PrecisionScale()
	if ok {
		t.Error("Expected ok=false for TIMESTAMP with unknown scale")
	}

	// INTEGER - not a precision/scale type.
	info = &TypeInfo{ValueType: ValueTypeInteger, Precision: 10, Scale: 0}
	_, _, ok = info.PrecisionScale()
	if ok {
		t.Error("Expected ok=false for INTEGER")
	}
}

// TestTypeInfo_ScanType tests the Go type hints reported for scanned values.
func TestTypeInfo_ScanType(t *testing.T) {
	tests := []struct {
		name string
		info *TypeInfo
		want reflect.Type
	}{
		{"bool", &TypeInfo{ValueType: ValueTypeBoolean}, reflect.TypeOf(true)},
		{"int64", &TypeInfo{ValueType: ValueTypeBigint}, reflect.TypeOf(int64(0))},
		{"float64", &TypeInfo{ValueType: ValueTypeDouble}, reflect.TypeOf(float64(0))},
		{"string-varchar", &TypeInfo{ValueType: ValueTypeVarchar}, reflect.TypeOf("")},
		{"string-numeric", &TypeInfo{ValueType: ValueTypeNumeric}, reflect.TypeOf("")},
		{"bytes", &TypeInfo{ValueType: ValueTypeVarbinary}, reflect.TypeOf([]byte(nil))},
		{"time", &TypeInfo{ValueType: ValueTypeTimestampTZ}, reflect.TypeOf(time.Time{})},
		{"string-array", &TypeInfo{ValueType: ValueTypeArray}, reflect.TypeOf("")},
		{"string-row", &TypeInfo{ValueType: ValueTypeRow}, reflect.TypeOf("")},
		{"string-interval", &TypeInfo{ValueType: ValueTypeInterval}, reflect.TypeOf("")},
		{"int64-enum", &TypeInfo{ValueType: ValueTypeEnum}, reflect.TypeOf(int64(0))},
		{"bytes-json", &TypeInfo{ValueType: ValueTypeJSON}, reflect.TypeOf([]byte(nil))},
		{"bytes-geometry", &TypeInfo{ValueType: ValueTypeGeometry}, reflect.TypeOf([]byte(nil))},
		{"bytes-javaobject", &TypeInfo{ValueType: ValueTypeJavaObject}, reflect.TypeOf([]byte(nil))},
		{"any-unsupported", &TypeInfo{ValueType: 999}, reflect.TypeOf((*any)(nil)).Elem()},
	}

	for _, tc := range tests {
		if got := tc.info.ScanType(); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
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
