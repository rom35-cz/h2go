package h2go

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
)

// roundTripWrite writes a value through Tr, then reads it back through another
// Tr connected via bytes.Buffer, verifying the wire format is self-consistent.
func roundTripWrite(t *testing.T, writeFn func(*Tr) error, readFn func(*Tr) error) {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&writeCloseBuffer{&buf})
	r := NewReader(&buf)

	if err := writeFn(w); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	if err := readFn(r); err != nil {
		t.Fatalf("read error: %v", err)
	}
}

type writeCloseBuffer struct {
	*bytes.Buffer
}

func (w *writeCloseBuffer) Close() error { return nil }

// ---- Bool ----

func TestTr_Bool(t *testing.T) {
	for _, val := range []bool{false, true} {
		t.Run(bl(val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteBool(val) },
				func(tr *Tr) error {
					got, err := tr.ReadBool()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadBool = %v, want %v", got, val)
					}
					return nil
				},
			)
		})
	}
}

func bl(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---- Byte ----

func TestTr_Byte(t *testing.T) {
	for _, val := range []byte{0, 1, 0x7F, 0x80, 0xFF} {
		t.Run(fmt.Sprintf("0x%02X", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteByte(val) },
				func(tr *Tr) error {
					got, err := tr.ReadByte()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadByte = %d, want %d", got, val)
					}
					return nil
				},
			)
		})
	}
}

// ---- Int16 ----

func TestTr_Int16(t *testing.T) {
	cases := []int16{0, 1, -1, 0x7F, -0x80, 0x7FFF, -0x8000}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%d", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteInt16(val) },
				func(tr *Tr) error {
					got, err := tr.ReadInt16()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadInt16 = %d, want %d", got, val)
					}
					return nil
				},
			)
		})
	}
}

// ---- Int32 ----

func TestTr_Int32(t *testing.T) {
	cases := []int32{0, 1, -1, 0x7F, -0x80, 0x7FFFFFFF, -0x80000000}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%d", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteInt32(val) },
				func(tr *Tr) error {
					got, err := tr.ReadInt32()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadInt32 = %d, want %d", got, val)
					}
					return nil
				},
			)
		})
	}
}

// ---- Int64 ----

func TestTr_Int64(t *testing.T) {
	cases := []int64{0, 1, -1, math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%d", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteInt64(val) },
				func(tr *Tr) error {
					got, err := tr.ReadInt64()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadInt64 = %d, want %d", got, val)
					}
					return nil
				},
			)
		})
	}
}

// ---- Float32 ----

func TestTr_Float32(t *testing.T) {
	cases := []float32{
		0, -0, 1, -1, 3.14, -3.14,
		math.MaxFloat32, math.SmallestNonzeroFloat32,
		float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN()),
	}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%v", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteFloat32(val) },
				func(tr *Tr) error {
					got, err := tr.ReadFloat32()
					if err != nil {
						return err
					}
					if !float32Eq(got, val) {
						t.Fatalf("ReadFloat32 = %v, want %v", got, val)
					}
					return nil
				},
			)
		})
	}
}

func float32Eq(a, b float32) bool {
	return a == b || (math.IsNaN(float64(a)) && math.IsNaN(float64(b)))
}

// ---- Float64 ----

func TestTr_Float64(t *testing.T) {
	cases := []float64{
		0, -0, 1, -1, 3.141592653589793, -3.141592653589793,
		math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.Inf(1), math.Inf(-1), math.NaN(),
	}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%v", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteFloat64(val) },
				func(tr *Tr) error {
					got, err := tr.ReadFloat64()
					if err != nil {
						return err
					}
					if !float64Eq(got, val) {
						t.Fatalf("ReadFloat64 = %v, want %v", got, val)
					}
					return nil
				},
			)
		})
	}
}

func float64Eq(a, b float64) bool {
	return a == b || (math.IsNaN(a) && math.IsNaN(b))
}

// ---- String ----

func TestTr_String_Empty(t *testing.T) {
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteString("") },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got == nil || *got != "" {
				t.Fatalf("ReadString = %v, want empty string", got)
			}
			return nil
		},
	)
}

func TestTr_String_Null(t *testing.T) {
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteNullString() },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got != nil {
				t.Fatalf("ReadString = %v, want nil (null)", got)
			}
			return nil
		},
	)
}

func TestTr_String_ASCII(t *testing.T) {
	val := "Hello, H2!"
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteString(val) },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got == nil || *got != val {
				t.Fatalf("ReadString = %q, want %q", *got, val)
			}
			return nil
		},
	)
}

func TestTr_String_NonASCII(t *testing.T) {
	val := "Hello, 世界! 👋"
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteString(val) },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got == nil || *got != val {
				t.Fatalf("ReadString = %q, want %q", *got, val)
			}
			return nil
		},
	)
}

func TestTr_String_NonBMP(t *testing.T) {
	val := "𝄞🎵🎶🔥"
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteString(val) },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got == nil || *got != val {
				t.Fatalf("ReadString = %q, want %q", *got, val)
			}
			return nil
		},
	)
}

func TestTr_String_Long(t *testing.T) {
	val := strings.Repeat("abc世", 1000) // ~6000 runes, mixed BMP+non-BMP
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteString(val) },
		func(tr *Tr) error {
			got, err := tr.ReadString()
			if err != nil {
				return err
			}
			if got == nil || *got != val {
				t.Fatalf("ReadString length mismatch: got %d, want %d",
					len(*got), len(val))
			}
			return nil
		},
	)
}

func TestTr_String_NullString(t *testing.T) {
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteNullString() },
		func(tr *Tr) error {
			s, err := tr.ReadStringPtr()
			if err != nil {
				return err
			}
			if s != "" {
				t.Fatalf("ReadStringPtr = %q, want empty for null", s)
			}
			return nil
		},
	)
}

// ---- Bytes ----

func TestTr_Bytes_Empty(t *testing.T) {
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteBytes([]byte{}) },
		func(tr *Tr) error {
			got, err := tr.ReadBytes()
			if err != nil {
				return err
			}
			if got == nil {
				t.Fatal("ReadBytes = nil, want empty slice")
			}
			if len(got) != 0 {
				t.Fatalf("ReadBytes length = %d, want 0", len(got))
			}
			return nil
		},
	)
}

func TestTr_Bytes_Nil(t *testing.T) {
	roundTripWrite(t,
		func(tr *Tr) error { return tr.WriteBytes(nil) },
		func(tr *Tr) error {
			got, err := tr.ReadBytes()
			if err != nil {
				return err
			}
			if got != nil {
				t.Fatal("ReadBytes = non-nil, want nil (null)")
			}
			return nil
		},
	)
}

func TestTr_Bytes_RoundTrip(t *testing.T) {
	vals := [][]byte{
		{},
		{0},
		{0, 1, 2, 0xFF},
		bytes.Repeat([]byte{'H', '2'}, 100),
	}
	for _, val := range vals {
		t.Run(fmt.Sprintf("len=%d", len(val)), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteBytes(val) },
				func(tr *Tr) error {
					got, err := tr.ReadBytes()
					if err != nil {
						return err
					}
					if !bytes.Equal(got, val) {
						t.Fatalf("ReadBytes = %v, want %v", got, val)
					}
					return nil
				},
			)
		})
	}
}

// TestTr_String_CapExceeded verifies that a length field beyond the wire cap
// is rejected before any large allocation happens.
func TestTr_String_CapExceeded(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, MaxWireLength/2+1) // chars; payload would be 2x this in bytes

	tr := mockTransferFromBytes(buf.Bytes())
	s, err := tr.ReadString()
	if err == nil {
		t.Fatalf("expected cap error, got string of len %d", len(*s))
	}
	if s != nil {
		t.Errorf("expected nil string, got len %d", len(*s))
	}
}

// TestTr_Bytes_CapExceeded verifies that a bytes length beyond the wire cap
// is rejected before any large allocation happens.
func TestTr_Bytes_CapExceeded(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, MaxWireLength+1)

	tr := mockTransferFromBytes(buf.Bytes())
	b, err := tr.ReadBytes()
	if err == nil {
		t.Fatalf("expected cap error, got %d bytes", len(b))
	}
	if b != nil {
		t.Errorf("expected nil bytes, got len %d", len(b))
	}
}

// ---- RowCount ----

func TestTr_RowCount(t *testing.T) {
	cases := []int64{0, 1, -1, math.MaxInt32, math.MaxInt64, math.MinInt64}
	for _, val := range cases {
		t.Run(fmt.Sprintf("%d", val), func(t *testing.T) {
			roundTripWrite(t,
				func(tr *Tr) error { return tr.WriteRowCount(val) },
				func(tr *Tr) error {
					got, err := tr.ReadRowCount()
					if err != nil {
						return err
					}
					if got != val {
						t.Fatalf("ReadRowCount = %d, want %d", got, val)
					}
					return nil
				},
			)
		})
	}
}

// ---- Mixed sequence ----

func TestTr_MixedSequence(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&writeCloseBuffer{&buf})
	r := NewReader(&buf)

	type op struct {
		name  string
		do    func(*Tr) error
		check func(*Tr) error
	}

	ops := []op{
		{"writeInt32(42)", func(tr *Tr) error { return tr.WriteInt32(42) },
			func(tr *Tr) error {
				v, err := tr.ReadInt32()
				if err != nil {
					return err
				}
				if v != 42 {
					return fmt.Errorf("got %d, want 42", v)
				}
				return nil
			},
		},
		{"writeString(hello)", func(tr *Tr) error { return tr.WriteString("hello") },
			func(tr *Tr) error {
				s, err := tr.ReadString()
				if err != nil {
					return err
				}
				if s == nil || *s != "hello" {
					return fmt.Errorf("got %v, want hello", *s)
				}
				return nil
			},
		},
		{"writeBool(true)", func(tr *Tr) error { return tr.WriteBool(true) },
			func(tr *Tr) error {
				v, err := tr.ReadBool()
				if err != nil {
					return err
				}
				if v != true {
					return fmt.Errorf("got %v, want true", v)
				}
				return nil
			},
		},
		{"writeFloat64(PI)", func(tr *Tr) error { return tr.WriteFloat64(math.Pi) },
			func(tr *Tr) error {
				v, err := tr.ReadFloat64()
				if err != nil {
					return err
				}
				if v != math.Pi {
					return fmt.Errorf("got %v, want %v", v, math.Pi)
				}
				return nil
			},
		},
		{"writeNullString", func(tr *Tr) error { return tr.WriteNullString() },
			func(tr *Tr) error {
				s, err := tr.ReadString()
				if err != nil {
					return err
				}
				if s != nil {
					return fmt.Errorf("got %v, want nil", *s)
				}
				return nil
			},
		},
		{"writeBytes(nil)", func(tr *Tr) error { return tr.WriteBytes(nil) },
			func(tr *Tr) error {
				b, err := tr.ReadBytes()
				if err != nil {
					return err
				}
				if b != nil {
					return fmt.Errorf("got %v, want nil", b)
				}
				return nil
			},
		},
		{"writeRowCount(999)", func(tr *Tr) error { return tr.WriteRowCount(999) },
			func(tr *Tr) error {
				v, err := tr.ReadRowCount()
				if err != nil {
					return err
				}
				if v != 999 {
					return fmt.Errorf("got %d, want 999", v)
				}
				return nil
			},
		},
	}

	for _, op := range ops {
		if err := op.do(w); err != nil {
			t.Fatalf("write %s: %v", op.name, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	for _, op := range ops {
		if err := op.check(r); err != nil {
			t.Fatalf("read %s: %v", op.name, err)
		}
	}
}

// ---- UTF-16 helper tests ----

func TestUTF16EncodeDecode(t *testing.T) {
	strings := []string{
		"",
		"hello",
		"Hello, 世界",
		"𝄞🎵🎶🔥",
		"a𝄞b🎵c🎶d🔥e",
		strings.Repeat("世", 100),
	}
	for _, s := range strings {
		t.Run(fmt.Sprintf("%q[:%d]", s, len(s)), func(t *testing.T) {
			units := utf16Encode(s)
			got := utf16Decode(units)
			if got != s {
				t.Fatalf("round-trip: got %q, want %q", got, s)
			}
		})
	}
}

func TestUTF16Decode_LoneSurrogates(t *testing.T) {
	cases := []struct {
		name  string
		units []uint16
		want  string
	}{
		{"lone high surrogate", []uint16{0xD800}, "\uFFFD"},
		{"lone low surrogate", []uint16{0xDC00}, "\uFFFD"},
		{"two high surrogates", []uint16{0xD800, 0xD800}, "\uFFFD\uFFFD"},
		{"high+invalid low", []uint16{0xD800, 0x0000}, "\uFFFD\u0000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := utf16Decode(tc.units)
			if got != tc.want {
				t.Errorf("utf16Decode(%v) = %q, want %q", tc.units, got, tc.want)
			}
		})
	}
}

// ---- Error cases ----

func TestTr_ReadFromEmptyBuffer(t *testing.T) {
	r := NewReader(new(bytes.Buffer))
	_, err := r.ReadByte()
	if err == nil {
		t.Fatal("expected error reading from empty buffer")
	}
}

func TestTr_ReadOnWriteOnly(t *testing.T) {
	w := NewWriter(&writeCloseBuffer{new(bytes.Buffer)})
	_, err := w.ReadByte()
	if err == nil {
		t.Fatal("expected error reading from write-only transfer")
	}
	if !strings.Contains(err.Error(), "write-only") {
		t.Errorf("error = %q, want message containing 'write-only'", err.Error())
	}
}

func TestTr_WriteOnReadOnly(t *testing.T) {
	r := NewReader(new(bytes.Buffer))
	err := r.WriteByte(0)
	if err == nil {
		t.Fatal("expected error writing to read-only transfer")
	}
}
