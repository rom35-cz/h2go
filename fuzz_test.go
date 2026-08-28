// fuzz_test.go — fuzz targets for the H2 wire decoder.
//
// These targets treat arbitrary bytes as a hostile or broken server stream and
// assert the decoder never panics, never performs an absurd allocation, and
// never consumes more input than was provided. The wire caps in transfer.go
// and typeinfo.go guard length fields before allocation, and Go's fuzzer
// enforces per-input time and memory limits, so any panic, hang, or
// catastrophic allocation surfaces as an immediate crash.
//
// The seed corpus runs as ordinary tests during `go test ./...`. Run a real
// fuzzing session with, for example:
//
//	go test -fuzz=FuzzReadValue -fuzztime=60s .
//
// Fuzz failures should be treated as decoder bugs: reproduce with
// `go test -run=FuzzReadValue -fuzz=...` and the recorded corpus file, then
// fix the decoder (never the test) unless the input was genuinely accepted.

package h2go

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fuzzTr builds a read-only Tr over b.
func fuzzTr(b []byte) *Tr { return NewReader(bytes.NewReader(b)) }

// writeSeed serializes a seed frame using the write side of the codec, so the
// seed corpus exercises exactly the byte layout the decoder expects.
func writeSeed(write func(*Tr)) []byte {
	var buf bytes.Buffer
	t := NewWriter(nopWriteCloser{&buf})
	write(t)
	if err := t.Flush(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// decodeValueStream runs a full decode-until-error pass over b for each
// column context. Every iteration consumes at least the 4-byte type code, so
// the loop is naturally bounded by len(b); the explicit cap is belt and
// braces against a future zero-consumption regression that would otherwise
// hang the fuzzer.
func decodeValueStream(tb testing.TB, b []byte) {
	tb.Helper()
	contexts := []*TypeInfo{
		// Plain type-code-driven decode (colType unused by most readers).
		nil,
		// Contexts that steer ARRAY/ROW element decoding.
		{ValueType: ValueTypeVarchar, Precision: 100},
		{ValueType: ValueTypeArray, Precision: 100, ExtTypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
		{ValueType: ValueTypeArray, Precision: 8,
			ExtTypeInfo: &TypeInfo{ValueType: ValueTypeArray, ExtTypeInfo: &TypeInfo{ValueType: ValueTypeVarchar}}},
		{ValueType: ValueTypeRow},
	}
	for _, c := range contexts {
		tr := fuzzTr(b)
		for i := 0; i < 4096; i++ {
			if _, err := tr.ReadValue(c); err != nil {
				break
			}
		}
	}
}

func FuzzReadValue(f *testing.F) {
	// Seed: a mixed row of distinct scalar types.
	f.Add(writeSeed(func(t *Tr) {
		fzWriteInt32(t, ValueTypeInteger, 42)
		fzWriteInt32(t, ValueTypeVarchar, 0) // ""
		fzWriteBool(t, true)
		fzWriteInt32(t, ValueTypeSmallint, -3)
		fzWriteNull(t)
		fzWriteInt64(t, ValueTypeBigint, 1<<40)
		fzWriteBytes(t, []byte{0x00, 0x01, 0xfe})
		fzWriteFloat64(t, 3.5)
	}))
	// Seed: ARRAY of three INTEGER elements.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeArray)
		t.WriteInt32(3)
		fzWriteInt32(t, ValueTypeInteger, 1)
		fzWriteInt32(t, ValueTypeInteger, 2)
		fzWriteInt32(t, ValueTypeInteger, 3)
	}))
	// Seed: nested ARRAY of ARRAY of VARCHAR.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeArray)
		t.WriteInt32(2)
		t.WriteInt32(ValueTypeArray)
		t.WriteInt32(1)
		t.WriteInt32(ValueTypeVarchar)
		t.WriteString("a")
		t.WriteInt32(ValueTypeArray)
		t.WriteInt32(0)
	}))
	// Seed: ROW of mixed values.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeRow)
		t.WriteInt32(3)
		fzWriteInt64(t, ValueTypeBigint, 77)
		fzWriteInt32(t, ValueTypeInteger, -1)
		fzWriteNull(t)
	}))
	// Seed: inline BLOB (length, payload, magic).
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeBlob)
		t.WriteInt64(2)
		t.WriteByte(0x01)
		t.WriteByte(0x02)
		t.WriteInt32(lobMagic)
	}))
	// Seed: inline CLOB (char length, chars, magic).
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeClob)
		t.WriteInt64(3)
		t.WriteByte('a')
		t.WriteByte('b')
		t.WriteByte('c')
		t.WriteInt32(lobMagic)
	}))
	// Seed: fetch-on-demand CLOB frame (tableID, lobID, hmac, lengths).
	// With no writer attached, fetchLob fails cleanly after parsing.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeClob)
		t.WriteInt64(-1)
		t.WriteInt32(1)
		t.WriteInt64(2)
		t.WriteBytes([]byte{0xaa})
		t.WriteInt64(3)
		t.WriteInt64(3)
	}))
	// Seed: UUID, TIME, DATE, TIMESTAMP pairs.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeUUID)
		t.WriteInt64(0x0102030405060708)
		t.WriteInt64(0x090a0b0c0d0e0f10)
		t.WriteInt32(ValueTypeTime)
		t.WriteInt64(1234567890)
		t.WriteInt32(ValueTypeDate)
		t.WriteInt64(20000)
		t.WriteInt32(ValueTypeTimestamp)
		t.WriteInt64(1700000000000)
		t.WriteInt64(999999999)
	}))
	// Seed: ENUM, JSON, GEOMETRY, DECFLOAT wire types (JSON/GEOMETRY are
	// byte-string values; DECFLOAT is a string that must parse).
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(ValueTypeEnum)
		t.WriteString("LOW")
		t.WriteInt32(ValueTypeJSON)
		fzWriteBytes(t, []byte(`{"a":1}`))
		t.WriteInt32(ValueTypeGeometry)
		fzWriteBytes(t, []byte{0x00})
		t.WriteInt32(ValueTypeDecfloat)
		t.WriteString("23.45")
	}))

	f.Fuzz(func(t *testing.T, data []byte) {
		decodeValueStream(t, data)
	})
}

func FuzzReadTypeInfo(f *testing.F) {
	// Seed: VARCHAR with precision.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIVarchar)
		t.WriteInt32(25)
	}))
	// Seed: NUMERIC with precision, scale, ext flag.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TINumeric)
		t.WriteInt32(10)
		t.WriteInt32(2)
		t.WriteBool(true)
	}))
	// Seed: ARRAY of VARCHAR.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIArray)
		t.WriteInt32(50)
		t.WriteInt32(TIVarchar)
		t.WriteInt32(10)
	}))
	// Seed: ENUM with three enumerators.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIEnum)
		t.WriteInt32(3)
		t.WriteString("a")
		t.WriteString("b")
		t.WriteString("c")
	}))
	// Seed: ROW with two fields.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIRow)
		t.WriteInt32(2)
		t.WriteString("f1")
		t.WriteInt32(TIBigint)
		t.WriteString("f2")
		t.WriteInt32(TIVarchar)
		t.WriteInt32(5)
	}))
	// Seed: GEOMETRY with type and SRID (3).
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIGeometry)
		t.WriteByte(3)
		t.WriteInt16(2)
		t.WriteInt32(4326)
	}))
	// Seed: INTERVAL DAY TO SECOND (precision + scale bytes).
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIIntervalDaySecond)
		t.WriteByte(3)
		t.WriteByte(6)
	}))
	// Seed: BLOB with int64 precision.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TIBlob)
		t.WriteInt64(1 << 20)
	}))
	// Seed: TIMESTAMP with scale byte.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(TITimestamp)
		t.WriteByte(6)
	}))

	f.Fuzz(func(t *testing.T, data []byte) {
		tr := fuzzTr(data)
		ti, err := tr.ReadTypeInfo()
		if err != nil {
			return
		}
		// A successfully decoded TypeInfo must be internally consistent.
		if ti == nil {
			t.Fatal("ReadTypeInfo returned nil TypeInfo with nil error")
		}
		if ti.ValueType == ValueTypeArray && ti.ExtTypeInfo == nil {
			t.Fatal("ARRAY TypeInfo decoded without element type")
		}
	})
}

// metaColumn writes one ResultMeta column frame (protocol 21 layout):
// alias, schema, table, column name, TypeInfo, identity, nullable.
func metaColumn(t *Tr, alias, schema, table, name string, ti int32, prec int32, identity bool, nullable int32) {
	_ = t.WriteString(alias)
	_ = t.WriteString(schema)
	_ = t.WriteString(table)
	_ = t.WriteString(name)
	_ = t.WriteInt32(ti)
	_ = t.WriteInt32(prec)
	_ = t.WriteBool(identity)
	_ = t.WriteInt32(nullable)
}

func FuzzReadResultMeta(f *testing.F) {
	// Seed: single-column meta. The count prefix is the first int32; the fuzz
	// body splits it off and feeds the rest to ReadResultMeta.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(1)
		metaColumn(t, "ID", "PUBLIC", "T", "ID", TIBigint, 0, true, ColumnNoNulls)
	}))
	// Seed: two-column meta.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(2)
		metaColumn(t, "A", "PUBLIC", "T", "ID", TIBigint, 0, true, ColumnNoNulls)
		metaColumn(t, "B", "PUBLIC", "T", "NAME", TIVarchar, 50, false, ColumnNullable)
	}))
	// Seed: hostile count; the guards must reject before allocating columns.
	f.Add(writeSeed(func(t *Tr) {
		t.WriteInt32(-1)
	}))

	f.Fuzz(func(t *testing.T, data []byte) {
		// columnCount is taken from the first four bytes so hostile counts
		// (negative or above the element cap) exercise the fail-fast guards.
		if len(data) < 4 {
			return
		}
		count := int32(binary.BigEndian.Uint32(data[:4]))
		tr := fuzzTr(data[4:])
		meta, err := tr.ReadResultMeta(count, TCPProtocolVersion21)
		if err != nil {
			return
		}
		if int(meta.ColumnCount) != int(count) {
			t.Fatalf("meta.ColumnCount %d != %d", meta.ColumnCount, count)
		}
		for i := range meta.Columns {
			if meta.Columns[i].TypeInfo == nil {
				t.Fatalf("column %d: nil TypeInfo", i)
			}
		}
	})
}

func FuzzReadPrimitives(f *testing.F) {
	f.Add([]byte{0, 0, 0, 5, 'h', 'e', 'l', 'l', 'o'})
	f.Add([]byte{0, 0, 0, 3, 0x01, 0x02, 0x03})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x7f, 0x7f, 0x7f, 0x7f})
	f.Add([]byte{0x00, 0x01})

	f.Fuzz(func(_ *testing.T, data []byte) {
		readers := []struct {
			name string
			fn   func(*Tr) error
		}{
			{"ReadString", func(tr *Tr) error { _, err := tr.ReadString(); return err }},
			{"ReadBytes", func(tr *Tr) error { _, err := tr.ReadBytes(); return err }},
			{"ReadInt64", func(tr *Tr) error { _, err := tr.ReadInt64(); return err }},
			{"ReadInt32", func(tr *Tr) error { _, err := tr.ReadInt32(); return err }},
			{"ReadBool", func(tr *Tr) error { _, err := tr.ReadBool(); return err }},
		}
		for _, r := range readers {
			tr := fuzzTr(data)
			for i := 0; i < 4096; i++ {
				if r.fn(tr) != nil {
					break
				}
			}
		}
	})
}

// Small helpers to keep seed builders readable.

func fzWriteInt32(t *Tr, typeCode int32, v int32) {
	_ = t.WriteInt32(typeCode)
	_ = t.WriteInt32(v)
}

func fzWriteInt64(t *Tr, typeCode int32, v int64) {
	_ = t.WriteInt32(typeCode)
	_ = t.WriteInt64(v)
}

func fzWriteBool(t *Tr, v bool) {
	_ = t.WriteInt32(ValueTypeBoolean)
	_ = t.WriteBool(v)
}

func fzWriteNull(t *Tr) {
	_ = t.WriteInt32(ValueTypeNull)
}

func fzWriteFloat64(t *Tr, v float64) {
	_ = t.WriteInt32(ValueTypeDouble)
	_ = t.WriteFloat64(v)
}

func fzWriteBytes(t *Tr, b []byte) {
	if b == nil {
		_ = t.WriteInt32(ValueTypeNull)
		return
	}
	_ = t.WriteInt32(ValueTypeVarbinary)
	_ = t.WriteBytes(b)
}
