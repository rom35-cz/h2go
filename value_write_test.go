package h2go

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func writeValueBytes(t *testing.T, v driver.Value, ti *TypeInfo) []byte {
	t.Helper()
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	if err := tr.WriteValue(v, ti); err != nil {
		t.Fatalf("WriteValue failed: %v", err)
	}
	if err := tr.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	return buf.Bytes()
}

func readTypeCode(t *testing.T, wire []byte) int32 {
	t.Helper()
	tr := mockTransferFromBytes(wire)
	vt, err := tr.ReadInt32()
	if err != nil {
		t.Fatalf("ReadInt32(type code) failed: %v", err)
	}
	return vt
}

func decodeWireValue(t *testing.T, wire []byte) driver.Value {
	t.Helper()
	tr := mockTransferFromBytes(wire)
	v, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue failed: %v", err)
	}
	return v
}

func TestWriteValue_RoundTrip_Primitives(t *testing.T) {
	tests := []struct {
		name  string
		input driver.Value
		check func(*testing.T, driver.Value)
	}{
		{
			name:  "nil",
			input: nil,
			check: func(t *testing.T, got driver.Value) {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
			},
		},
		{
			name:  "bool",
			input: true,
			check: func(t *testing.T, got driver.Value) {
				if v, ok := got.(bool); !ok || !v {
					t.Errorf("got %T(%v), want bool(true)", got, got)
				}
			},
		},
		{
			name:  "int64",
			input: int64(-123456789),
			check: func(t *testing.T, got driver.Value) {
				if v, ok := got.(int64); !ok || v != -123456789 {
					t.Errorf("got %T(%v), want int64(-123456789)", got, got)
				}
			},
		},
		{
			name:  "float64",
			input: 3.1415926535,
			check: func(t *testing.T, got driver.Value) {
				v, ok := got.(float64)
				if !ok {
					t.Fatalf("got %T(%v), want float64", got, got)
				}
				if math.Abs(v-3.1415926535) > 1e-9 {
					t.Errorf("got %v, want 3.1415926535", v)
				}
			},
		},
		{
			name:  "string",
			input: "hello h2",
			check: func(t *testing.T, got driver.Value) {
				if v, ok := got.(string); !ok || v != "hello h2" {
					t.Errorf("got %T(%v), want string(hello h2)", got, got)
				}
			},
		},
		{
			name:  "bytes",
			input: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			check: func(t *testing.T, got driver.Value) {
				v, ok := got.([]byte)
				if !ok {
					t.Fatalf("got %T(%v), want []byte", got, got)
				}
				if !bytes.Equal(v, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
					t.Errorf("got %X, want DEADBEEF", v)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wire := writeValueBytes(t, tc.input, nil)
			got := decodeWireValue(t, wire)
			tc.check(t, got)
		})
	}
}

func TestWriteValue_NumericTypeInfo(t *testing.T) {
	tests := []struct {
		name  string
		input driver.Value
		ti    *TypeInfo
		want  string
		vt    int32
	}{
		{name: "string numeric", input: "12345.6789", ti: &TypeInfo{ValueType: ValueTypeNumeric}, want: "12345.6789", vt: ValueTypeNumeric},
		{name: "int64 numeric", input: int64(12345), ti: &TypeInfo{ValueType: ValueTypeNumeric}, want: "12345", vt: ValueTypeNumeric},
		{name: "float64 numeric", input: float64(123.5), ti: &TypeInfo{ValueType: ValueTypeNumeric}, want: "123.5", vt: ValueTypeNumeric},
		{name: "int64 decfloat", input: int64(-99), ti: &TypeInfo{ValueType: ValueTypeDecfloat}, want: "-99", vt: ValueTypeDecfloat},
		{name: "float64 decfloat", input: float64(1.25), ti: &TypeInfo{ValueType: ValueTypeDecfloat}, want: "1.25", vt: ValueTypeDecfloat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wire := writeValueBytes(t, tc.input, tc.ti)
			if vt := readTypeCode(t, wire); vt != tc.vt {
				t.Fatalf("type code: got %d, want %d", vt, tc.vt)
			}
			got := decodeWireValue(t, wire)
			if s, ok := got.(string); !ok || s != tc.want {
				t.Fatalf("decoded: got %T(%v), want string(%s)", got, got, tc.want)
			}
		})
	}
}

func TestWriteValue_StringWithUUIDTypeInfo(t *testing.T) {
	in := "12345678-1234-5678-9ABC-DEF012345678"
	wire := writeValueBytes(t, in, &TypeInfo{ValueType: ValueTypeUUID})
	if vt := readTypeCode(t, wire); vt != ValueTypeUUID {
		t.Fatalf("type code: got %d, want UUID(%d)", vt, ValueTypeUUID)
	}
	got := decodeWireValue(t, wire)
	if s, ok := got.(string); !ok || s != strings.ToLower(in) {
		t.Fatalf("decoded: got %T(%v), want %q", got, got, strings.ToLower(in))
	}
}

func TestWriteValue_StringWithUUIDTypeInfo_Invalid(t *testing.T) {
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	err := tr.WriteValue("not-a-uuid", &TypeInfo{ValueType: ValueTypeUUID})
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("error: got %v, want contains 'invalid UUID'", err)
	}
}

func TestWriteValue_TimeDefaultIsTimestamp(t *testing.T) {
	in := time.Date(2026, 7, 9, 13, 37, 5, 123456789, time.FixedZone("+02", 2*3600))
	wire := writeValueBytes(t, in, nil)
	if vt := readTypeCode(t, wire); vt != ValueTypeTimestamp {
		t.Fatalf("type code: got %d, want TIMESTAMP(%d)", vt, ValueTypeTimestamp)
	}
	got := decodeWireValue(t, wire).(time.Time)
	if got.Year() != 2026 || got.Month() != time.July || got.Day() != 9 {
		t.Fatalf("date: got %v, want 2026-07-09", got)
	}
	if got.Hour() != 13 || got.Minute() != 37 || got.Second() != 5 || got.Nanosecond() != 123456789 {
		t.Fatalf("clock: got %02d:%02d:%02d.%09d",
			got.Hour(), got.Minute(), got.Second(), got.Nanosecond())
	}
}

func TestWriteValue_TimeWithTypeInfo(t *testing.T) {
	in := time.Date(2026, 7, 9, 13, 37, 5, 987654321, time.FixedZone("+05", 5*3600))
	tests := []struct {
		name     string
		ti       *TypeInfo
		wantType int32
		check    func(*testing.T, time.Time)
	}{
		{
			name:     "DATE",
			ti:       &TypeInfo{ValueType: ValueTypeDate},
			wantType: ValueTypeDate,
			check: func(t *testing.T, got time.Time) {
				if got.Year() != 2026 || got.Month() != time.July || got.Day() != 9 {
					t.Fatalf("got %v, want date 2026-07-09", got)
				}
			},
		},
		{
			name:     "TIME",
			ti:       &TypeInfo{ValueType: ValueTypeTime},
			wantType: ValueTypeTime,
			check: func(t *testing.T, got time.Time) {
				if got.Hour() != 13 || got.Minute() != 37 || got.Second() != 5 || got.Nanosecond() != 987654321 {
					t.Fatalf("got %02d:%02d:%02d.%09d, want 13:37:05.987654321",
						got.Hour(), got.Minute(), got.Second(), got.Nanosecond())
				}
			},
		},
		{
			name:     "TIMESTAMP",
			ti:       &TypeInfo{ValueType: ValueTypeTimestamp},
			wantType: ValueTypeTimestamp,
			check: func(t *testing.T, got time.Time) {
				if got.Year() != 2026 || got.Month() != time.July || got.Day() != 9 {
					t.Fatalf("got %v, want date 2026-07-09", got)
				}
				if got.Hour() != 13 || got.Minute() != 37 || got.Second() != 5 || got.Nanosecond() != 987654321 {
					t.Fatalf("got %02d:%02d:%02d.%09d",
						got.Hour(), got.Minute(), got.Second(), got.Nanosecond())
				}
			},
		},
		{
			name:     "TIME_TZ",
			ti:       &TypeInfo{ValueType: ValueTypeTimeTZ},
			wantType: ValueTypeTimeTZ,
			check: func(t *testing.T, got time.Time) {
				if got.Hour() != 13 || got.Minute() != 37 || got.Second() != 5 {
					t.Fatalf("got %02d:%02d:%02d", got.Hour(), got.Minute(), got.Second())
				}
				_, off := got.Zone()
				if off != 5*3600 {
					t.Fatalf("offset: got %d, want 18000", off)
				}
			},
		},
		{
			name:     "TIMESTAMP_TZ",
			ti:       &TypeInfo{ValueType: ValueTypeTimestampTZ},
			wantType: ValueTypeTimestampTZ,
			check: func(t *testing.T, got time.Time) {
				if got.Year() != 2026 || got.Month() != time.July || got.Day() != 9 {
					t.Fatalf("got %v, want date 2026-07-09", got)
				}
				if got.Hour() != 13 || got.Minute() != 37 || got.Second() != 5 {
					t.Fatalf("got %02d:%02d:%02d", got.Hour(), got.Minute(), got.Second())
				}
				_, off := got.Zone()
				if off != 5*3600 {
					t.Fatalf("offset: got %d, want 18000", off)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wire := writeValueBytes(t, in, tc.ti)
			if vt := readTypeCode(t, wire); vt != tc.wantType {
				t.Fatalf("type code: got %d, want %d", vt, tc.wantType)
			}
			got := decodeWireValue(t, wire).(time.Time)
			tc.check(t, got)
		})
	}
}

func TestWriteValue_TypedNilBytesEncodesNull(t *testing.T) {
	var b []byte
	wire := writeValueBytes(t, b, nil)
	if vt := readTypeCode(t, wire); vt != ValueTypeNull {
		t.Fatalf("type code: got %d, want NULL(%d)", vt, ValueTypeNull)
	}
	if got := decodeWireValue(t, wire); got != nil {
		t.Fatalf("decoded: got %T(%v), want nil", got, got)
	}
}

func TestWriteValue_UnsupportedType(t *testing.T) {
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	err := tr.WriteValue(complex(1, 2), nil)
	if err == nil {
		t.Fatal("expected unsupported type error")
	}
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected ErrUnsupportedType, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "unsupported driver.Value type") {
		t.Fatalf("error: got %v, want unsupported-type message", err)
	}
}

func TestParseUUIDString(t *testing.T) {
	high, low, err := parseUUIDString("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("parseUUIDString failed: %v", err)
	}
	if got := formatUUID(high, low); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("round-trip uuid: got %s", got)
	}
}
