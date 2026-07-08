// value_read_test.go — H2 wire value decoder tests.

package h2go

import (
	"bytes"
	"math"
	"testing"
	"time"
)

// writeValueType writes a type code for testing.
func writeValueType(buf *bytes.Buffer, vt int32) {
	writeInt32(buf, vt)
}

// TestReadValue_Null tests reading a NULL value.
func TestReadValue_Null(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeNull)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	if val != nil {
		t.Errorf("Expected nil, got %v", val)
	}
}

// TestReadValue_Boolean tests reading BOOLEAN values.
func TestReadValue_Boolean(t *testing.T) {
	tests := []struct {
		input bool
		want  bool
	}{
		{true, true},
		{false, false},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeBoolean)
		writeByte(buf, boolByte(tc.input))

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(bool)
		if !ok {
			t.Fatalf("Expected bool, got %T", val)
		}
		if got != tc.want {
			t.Errorf("ReadValue: got %v, want %v", got, tc.want)
		}
	}
}

// TestReadValue_Tinyint tests reading TINYINT values.
func TestReadValue_Tinyint(t *testing.T) {
	tests := []struct {
		input byte
		want  int64
	}{
		{0, 0},
		{1, 1},
		{127, 127},
		{128, -128}, // signed byte
		{255, -1},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeTinyint)
		writeByte(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(int64)
		if !ok {
			t.Fatalf("Expected int64, got %T", val)
		}
		if got != tc.want {
			t.Errorf("ReadValue(0x%02x): got %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Smallint tests reading SMALLINT values.
func TestReadValue_Smallint(t *testing.T) {
	tests := []struct {
		input int16
		want  int64
	}{
		{0, 0},
		{1, 1},
		{32767, 32767},
		{-1, -1},
		{-32768, -32768},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeSmallint)
		writeInt16(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(int64)
		if !ok {
			t.Fatalf("Expected int64, got %T", val)
		}
		if got != tc.want {
			t.Errorf("ReadValue(%d): got %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Integer tests reading INTEGER values.
func TestReadValue_Integer(t *testing.T) {
	tests := []struct {
		input int32
		want  int64
	}{
		{0, 0},
		{1, 1},
		{2147483647, 2147483647},
		{-1, -1},
		{-2147483648, -2147483648},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeInteger)
		writeInt32(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(int64)
		if !ok {
			t.Fatalf("Expected int64, got %T", val)
		}
		if got != tc.want {
			t.Errorf("ReadValue(%d): got %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Bigint tests reading BIGINT values.
func TestReadValue_Bigint(t *testing.T) {
	tests := []struct {
		input int64
		want  int64
	}{
		{0, 0},
		{1, 1},
		{9223372036854775807, 9223372036854775807},
		{-1, -1},
		{-9223372036854775808, -9223372036854775808},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeBigint)
		writeInt64(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(int64)
		if !ok {
			t.Fatalf("Expected int64, got %T", val)
		}
		if got != tc.want {
			t.Errorf("ReadValue(%d): got %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Real tests reading REAL values.
func TestReadValue_Real(t *testing.T) {
	tests := []struct {
		input float32
		want  float64
	}{
		{0.0, 0.0},
		{1.0, 1.0},
		{3.14, 3.14},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeReal)
		writeFloat32(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(float64)
		if !ok {
			t.Fatalf("Expected float64, got %T", val)
		}
		// Use tolerance for float comparison
		if math.Abs(got-tc.want) > 0.001 {
			t.Errorf("ReadValue(%f): got %f, want %f", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Double tests reading DOUBLE values.
func TestReadValue_Double(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{0.0, 0.0},
		{1.0, 1.0},
		{2.718281828, 2.718281828},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeDouble)
		writeFloat64(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(float64)
		if !ok {
			t.Fatalf("Expected float64, got %T", val)
		}
		// Use tolerance for float comparison
		if math.Abs(got-tc.want) > 0.000001 {
			t.Errorf("ReadValue(%f): got %f, want %f", tc.input, got, tc.want)
		}
	}
}

// TestReadValue_Varchar tests reading VARCHAR values.
func TestReadValue_Varchar(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"hello"},
		{"world"},
		{""},
		{"Hello, World!"},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeVarchar)
		writeString(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(string)
		if !ok {
			t.Fatalf("Expected string, got %T", val)
		}
		if got != tc.input {
			t.Errorf("ReadValue(%q): got %q, want %q", tc.input, got, tc.input)
		}
	}
}

// TestReadValue_Varbinary tests reading VARBINARY values.
func TestReadValue_Varbinary(t *testing.T) {
	tests := []struct {
		input []byte
	}{
		{[]byte{0x00, 0x01, 0x02}},
		{[]byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{[]byte{}},
		{[]byte{0xFF}},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeVarbinary)
		writeBytes(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.([]byte)
		if !ok {
			t.Fatalf("Expected []byte, got %T", val)
		}
		if !bytes.Equal(got, tc.input) {
			t.Errorf("ReadValue(%x): got %x, want %x", tc.input, got, tc.input)
		}
	}
}

// TestReadValue_Numeric tests reading NUMERIC values as strings.
func TestReadValue_Numeric(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"12345.67890"},
		{"-999.99"},
		{"0.00"},
	}

	for _, tc := range tests {
		buf := new(bytes.Buffer)
		writeValueType(buf, ValueTypeNumeric)
		writeString(buf, tc.input)

		tr := mockTransferFromBytes(buf.Bytes())
		val, err := tr.ReadValue(nil)
		if err != nil {
			t.Fatalf("ReadValue failed: %v", err)
		}
		got, ok := val.(string)
		if !ok {
			t.Fatalf("Expected string, got %T", val)
		}
		if got != tc.input {
			t.Errorf("ReadValue(%q): got %q, want %q", tc.input, got, tc.input)
		}
	}
}

// TestReadValue_UUID tests reading UUID values.
func TestReadValue_UUID(t *testing.T) {
	// Using UUID with values within int64 range
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeUUID)
	writeInt64(buf, 0x550e8400e29b41d4)
	writeInt64(buf, 0x0716446655440001)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got, ok := val.(string)
	if !ok {
		t.Fatalf("Expected string, got %T", val)
	}
	want := "550e8400-e29b-41d4-0716-446655440001"
	if got != want {
		t.Errorf("ReadValue: got %q, want %q", got, want)
	}
}

// TestFormatUUID tests UUID formatting.
func TestFormatUUID(t *testing.T) {
	tests := []struct {
		high, low int64
		want      string
	}{
		{0, 0, "00000000-0000-0000-0000-000000000000"},
		{0x550e8400e29b41d4, 0x0716446655440001, "550e8400-e29b-41d4-0716-446655440001"},
		{-1, -1, "ffffffff-ffff-ffff-ffff-ffffffffffff"},
	}

	for _, tc := range tests {
		got := formatUUID(tc.high, tc.low)
		if got != tc.want {
			t.Errorf("formatUUID(%#x, %#x): got %q, want %q", tc.high, tc.low, got, tc.want)
		}
	}
}

// TestReadValue_Date tests reading DATE values (basic smoke test).
func TestReadValue_Date(t *testing.T) {
	// Just verify DATE type is parsed without error
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeDate)
	writeInt64(buf, 0) // days since epoch = 1970-01-01

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	_, ok := val.(time.Time)
	if !ok {
		t.Fatalf("Expected time.Time, got %T", val)
	}
}

// TestReadValue_Time tests reading TIME values.
func TestReadValue_Time(t *testing.T) {
	// 13:45:00 in nanoseconds = (13*3600 + 45*60) * 1e9 = 49500000000000
	nanos := int64(13*3600+45*60) * 1e9
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTime)
	writeInt64(buf, nanos)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got, ok := val.(time.Time)
	if !ok {
		t.Fatalf("Expected time.Time, got %T", val)
	}

	h, m, s := got.Clock()
	if h != 13 || m != 45 || s != 0 {
		t.Errorf("Time: got %02d:%02d:%02d, want 13:45:00", h, m, s)
	}
}

// TestReadValue_Timestamp tests reading TIMESTAMP values (basic smoke test).
func TestReadValue_Timestamp(t *testing.T) {
	// Just verify TIMESTAMP type is parsed without error
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTimestamp)
	writeInt64(buf, 0) // dateValue
	writeInt64(buf, 0) // nanos

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	_, ok := val.(time.Time)
	if !ok {
		t.Fatalf("Expected time.Time, got %T", val)
	}
}

// TestValueTypeName tests type name strings.
func TestValueTypeName(t *testing.T) {
	tests := []struct {
		vt   int32
		want string
	}{
		{ValueTypeNull, "NULL"},
		{ValueTypeBoolean, "BOOLEAN"},
		{ValueTypeInteger, "INTEGER"},
		{ValueTypeBigint, "BIGINT"},
		{ValueTypeVarchar, "VARCHAR"},
		{ValueTypeTimestamp, "TIMESTAMP"},
		{ValueTypeBlob, "BLOB"},
		{999, "UNKNOWN(999)"},
	}

	for _, tc := range tests {
		got := valueTypeName(tc.vt)
		if got != tc.want {
			t.Errorf("valueTypeName(%d): got %q, want %q", tc.vt, got, tc.want)
		}
	}
}

// Helper functions for building test data

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func writeInt16(buf *bytes.Buffer, v int16) {
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
}

func writeFloat32(buf *bytes.Buffer, v float32) {
	bits := math.Float32bits(v)
	buf.WriteByte(byte(bits >> 24))
	buf.WriteByte(byte(bits >> 16))
	buf.WriteByte(byte(bits >> 8))
	buf.WriteByte(byte(bits))
}

func writeFloat64(buf *bytes.Buffer, v float64) {
	bits := math.Float64bits(v)
	for i := 7; i >= 0; i-- {
		buf.WriteByte(byte(bits >> (i * 8)))
	}
}

func writeBytes(buf *bytes.Buffer, b []byte) {
	if b == nil {
		writeInt32(buf, -1)
		return
	}
	writeInt32(buf, int32(len(b)))
	buf.Write(b)
}
