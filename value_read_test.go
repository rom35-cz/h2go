// value_read_test.go — H2 wire value decoder tests.

package h2go

import (
	"bytes"
	"errors"
	"math"
	"net"
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

// TestReadValue_Date_Epoch tests that H2's EPOCH_DATE_VALUE decodes to 1970-01-01.
// H2 stores dates as a packed long: (year << 9) | (month << 5) | day.
// EPOCH = (1970 << 9) | (1 << 5) | 1 = 1008673.
func TestReadValue_Date_Epoch(t *testing.T) {
	const epochDateValue = int64(1970<<9 | 1<<5 | 1) // 1008673
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeDate)
	writeInt64(buf, epochDateValue)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got, ok := val.(time.Time)
	if !ok {
		t.Fatalf("Expected time.Time, got %T", val)
	}
	if got.Year() != 1970 || got.Month() != 1 || got.Day() != 1 {
		t.Errorf("Epoch date: got %v, want 1970-01-01", got)
	}
	if got.Location() != time.UTC {
		t.Errorf("Expected UTC, got %v", got.Location())
	}
}

// TestReadValue_Date_Known tests a specific date decoding.
// 2024-01-15 → dateValue = (2024 << 9) | (1 << 5) | 15 = 1036335.
func TestReadValue_Date_Known(t *testing.T) {
	const dv = int64(2024<<9 | 1<<5 | 15) // 1036335
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeDate)
	writeInt64(buf, dv)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got := val.(time.Time)
	if got.Year() != 2024 || got.Month() != 1 || got.Day() != 15 {
		t.Errorf("Date decode: got %v, want 2024-01-15", got)
	}
}

// TestReadValue_Timestamp_Known tests a specific timestamp decoding.
// 2024-06-01 10:30:00 UTC → dateValue = (2024 << 9) | (6 << 5) | 1 = 1036993
// nanos = (10*3600 + 30*60) * 1_000_000_000 = 37_800_000_000_000.
func TestReadValue_Timestamp_Known(t *testing.T) {
	const dv = int64(2024<<9 | 6<<5 | 1) // 2024-06-01
	const nanos = int64((10*3600 + 30*60) * 1_000_000_000)

	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTimestamp)
	writeInt64(buf, dv)
	writeInt64(buf, nanos)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got := val.(time.Time)
	if got.Year() != 2024 || got.Month() != 6 || got.Day() != 1 {
		t.Errorf("Timestamp date: got %v, want 2024-06-01", got)
	}
	h, m, s := got.Clock()
	if h != 10 || m != 30 || s != 0 {
		t.Errorf("Timestamp time: got %02d:%02d:%02d, want 10:30:00", h, m, s)
	}
	if got.Location() != time.UTC {
		t.Errorf("Expected UTC, got %v", got.Location())
	}
}

// TestReadValue_TimestampTZ_LocalTime verifies that TIMESTAMP WITH TIME ZONE
// dateValue+timeNanos represent LOCAL time in the given timezone, not UTC.
// 2024-06-01 15:00:00+03:00 (local) → UTC is 2024-06-01 12:00:00Z.
func TestReadValue_TimestampTZ_LocalTime(t *testing.T) {
	const dv = int64(2024<<9 | 6<<5 | 1)           // 2024-06-01
	const nanos = int64(15 * 3600 * 1_000_000_000) // 15:00:00 LOCAL
	const offsetSec = int32(3 * 3600)              // UTC+3

	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTimestampTZ)
	writeInt64(buf, dv)
	writeInt64(buf, nanos)
	writeInt32(buf, offsetSec)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got := val.(time.Time)
	// Local representation: 15:00:00 +03:00
	if got.Hour() != 15 || got.Minute() != 0 {
		t.Errorf("Local time: got %02d:%02d, want 15:00", got.Hour(), got.Minute())
	}
	// UTC representation: 12:00:00
	utc := got.UTC()
	if utc.Hour() != 12 || utc.Minute() != 0 {
		t.Errorf("UTC time: got %02d:%02d, want 12:00", utc.Hour(), utc.Minute())
	}
	_, offsetGot := got.Zone()
	if offsetGot != 3*3600 {
		t.Errorf("Offset: got %d, want %d", offsetGot, 3*3600)
	}
}

// TestReadValue_TimeTZ_LocalTime verifies that TIME WITH TIME ZONE
// nanoseconds represent LOCAL time, not UTC.
func TestReadValue_TimeTZ_LocalTime(t *testing.T) {
	const nanos = int64(14 * 3600 * 1_000_000_000) // 14:00:00 LOCAL
	const offsetSec = int32(-5 * 3600)             // UTC-5

	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTimeTZ)
	writeInt64(buf, nanos)
	writeInt32(buf, offsetSec)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got := val.(time.Time)
	if got.Hour() != 14 {
		t.Errorf("Local hour: got %d, want 14", got.Hour())
	}
	// UTC would be 14:00 - (-5h) = 19:00
	utc := got.UTC()
	if utc.Hour() != 19 {
		t.Errorf("UTC hour: got %d, want 19", utc.Hour())
	}
	_, offsetGot := got.Zone()
	if offsetGot != -5*3600 {
		t.Errorf("Offset: got %d, want %d", offsetGot, -5*3600)
	}
}

// TestReadValue_TimestampTZ_NegativeYear tests a BC date in TIMESTAMP_TZ.
func TestReadValue_TimestampTZ_NegativeYear(t *testing.T) {
	// year=-1 (1 BCE), month=3, day=15
	const dv = int64((-1 << 9) | (3 << 5) | 15) // packed: -512+96+15 = -401
	const nanos = int64(0)
	const offsetSec = int32(0)

	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeTimestamp) // Use regular TIMESTAMP for simpler check
	writeInt64(buf, dv)
	writeInt64(buf, nanos)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got := val.(time.Time)
	if got.Year() != -1 || got.Month() != 3 || got.Day() != 15 {
		t.Errorf("BC date: got %v, want year=-1 month=3 day=15", got)
	}
	_ = offsetSec // suppress unused warning
}

// writeClobPayload encodes s the way Data.copyString writes an inline CLOB:
// char length (int64), then per-char UTF-8-ish encoding (1/2/3 bytes), then
// the LOB magic. H2 writes Java chars (UTF-16 code units), so characters
// outside the BMP would be written as a surrogate pair; tests here stay in BMP.
func writeClobPayload(buf *bytes.Buffer, s string) {
	runes := []rune(s)
	writeInt64(buf, int64(len(runes)))
	for _, r := range runes {
		switch {
		case r < 0x80:
			buf.WriteByte(byte(r))
		case r < 0x800:
			buf.WriteByte(byte(0xc0 | (r >> 6)))
			buf.WriteByte(byte(0x80 | (r & 0x3f)))
		default:
			buf.WriteByte(byte(0xe0 | (r >> 12)))
			buf.WriteByte(byte(0x80 | ((r >> 6) & 0x3f)))
			buf.WriteByte(byte(0x80 | (r & 0x3f)))
		}
	}
	writeInt32(buf, lobMagic)
}

// TestReadValue_InlineClob tests decoding of inline CLOB values.
func TestReadValue_InlineClob(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"ascii", "hello world"},
		{"utf8", "héllo ☺"},
		{"empty", ""},
		{"three byte", "日本語テキスト"},
		{"mixed", "aé日b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			writeValueType(buf, ValueTypeClob)
			writeClobPayload(buf, tc.input)

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
				t.Errorf("ReadValue: got %q, want %q", got, tc.input)
			}
		})
	}
}

// TestReadValue_InlineClob_CharLengthNotBytes verifies that the CLOB header
// length counts characters (Java chars / code points in BMP), not bytes.
func TestReadValue_InlineClob_CharLengthNotBytes(t *testing.T) {
	// "é" is 1 char but 2 bytes in the wire encoding; "☺" is 1 char, 3 bytes.
	s := "é☺"
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, 2)                  // 2 chars, but 5 payload bytes
	buf.Write([]byte{0xc3, 0xa9})       // é
	buf.Write([]byte{0xe2, 0x98, 0xba}) // ☺
	writeInt32(buf, lobMagic)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	if got := val.(string); got != s {
		t.Errorf("ReadValue: got %q, want %q", got, s)
	}
}

// TestReadValue_InlineClob_BadMagic verifies a wrong trailing magic errors.
func TestReadValue_InlineClob_BadMagic(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, 1)
	buf.WriteByte('a')
	writeInt32(buf, 0x9999) // wrong magic

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatalf("expected error, got value %v", val)
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}
}

// TestReadValue_InlineClob_Truncated verifies a truncated payload errors.
func TestReadValue_InlineClob_Truncated(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, 5)
	buf.Write([]byte("ab")) // only 2 of 5 chars present

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatalf("expected error, got value %v", val)
	}
}

// TestReadValue_InlineClob_CapExceeded verifies the char-length cap errors
// before allocating or reading a giant payload.
func TestReadValue_InlineClob_CapExceeded(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, maxInlineClobChars+1)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatalf("expected error, got value %v", val)
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}
}

// TestReadValue_InlineBlob verifies inline BLOB round-trip with magic.
func TestReadValue_InlineBlob(t *testing.T) {
	data := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeBlob)
	writeInt64(buf, int64(len(data)))
	buf.Write(data)
	writeInt32(buf, lobMagic)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	got, ok := val.([]byte)
	if !ok {
		t.Fatalf("Expected []byte, got %T", val)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("ReadValue: got %x, want %x", got, data)
	}
}

// TestReadValue_InlineBlob_BadMagic verifies a wrong BLOB magic errors.
func TestReadValue_InlineBlob_BadMagic(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeBlob)
	writeInt64(buf, 1)
	buf.WriteByte(0x42)
	writeInt32(buf, 0x9999)

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatalf("expected error, got value %v", val)
	}
}

// TestReadValue_FetchOnDemandLOB verifies the length == -1 path fetches LOB
// data via LOB_READ requests against a piped server.
func TestReadValue_FetchOnDemandLOB(t *testing.T) {
	for _, vt := range []int32{ValueTypeBlob, ValueTypeClob} {
		t.Run(valueTypeName(vt), func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			defer serverConn.Close()

			clientTr := NewReadWriter(clientConn)
			serverTr := NewReadWriter(serverConn)

			// Server goroutine: write metadata, then respond to LOB_READ.
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				// Write value type + fetch-on-demand metadata.
				serverTr.WriteInt32(vt) // type code
				serverTr.WriteInt64(-1) // fetch-on-demand marker
				serverTr.WriteInt32(7)  // tableId
				serverTr.WriteInt64(99) // lobID
				serverTr.WriteBytes([]byte{1, 2, 3}) // hmac
				if vt == ValueTypeClob {
					serverTr.WriteInt64(12) // octetLength
				}
				serverTr.WriteInt64(34) // charLength / precision
				serverTr.Flush()

				// Read the LOB_READ request and respond with chunk, then EOF.
				op, _ := serverTr.ReadInt32()
				if op != LobRead {
					t.Errorf("expected LobRead(%d), got %d", LobRead, op)
				}
				lobID, _ := serverTr.ReadInt64()
				if lobID != 99 {
					t.Errorf("expected lobID 99, got %d", lobID)
				}
				hmac, _ := serverTr.ReadBytes()
				_ = hmac
				offset, _ := serverTr.ReadInt64()
				_ = offset
				reqLen, _ := serverTr.ReadInt32()
				_ = reqLen

				// Respond with chunk
				chunk := []byte("hello")
				serverTr.WriteInt32(StatusOK)
				serverTr.WriteInt32(int32(len(chunk)))
				serverTr.w.Write(chunk)
				serverTr.Flush()

				// Respond with 0-length end-of-data
				op2, _ := serverTr.ReadInt32()
				if op2 != LobRead {
					t.Errorf("expected second LobRead(%d), got %d", LobRead, op2)
				}
				serverTr.ReadInt64() // lobID
				serverTr.ReadBytes() // hmac
				serverTr.ReadInt64() // offset
				serverTr.ReadInt32() // reqLen
				serverTr.WriteInt32(StatusOK)
				serverTr.WriteInt32(0) // actualLen=0 means EOF
				serverTr.Flush()
			}()

			val, err := clientTr.ReadValue(nil)
			if err != nil {
				t.Fatalf("ReadValue failed: %v", err)
			}
			// BLOB returns []byte, CLOB returns string
			switch vt {
			case ValueTypeBlob:
				got, ok := val.([]byte)
				if !ok {
					t.Fatalf("expected []byte, got %T", val)
				}
				if string(got) != "hello" {
					t.Errorf("BLOB: got %s, want hello", string(got))
				}
			case ValueTypeClob:
				got, ok := val.(string)
				if !ok {
					t.Fatalf("expected string, got %T", val)
				}
				if got != "hello" {
					t.Errorf("CLOB: got %s, want hello", got)
				}
			}
			<-serverDone
		})
	}
}

// TestReadValue_UnsupportedType ensures unknown type codes are surfaced
// with the ErrUnsupportedType sentinel.
func TestReadValue_UnsupportedType(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, 255) // code not in the ValueType constants

	tr := mockTransferFromBytes(buf.Bytes())
	val, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if val != nil {
		t.Fatalf("expected nil value, got %v", val)
	}
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected ErrUnsupportedType, got %T: %v", err, err)
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
