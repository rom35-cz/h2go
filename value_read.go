// value_read.go — H2 wire value decoder (protocol 21).
//
// Reference: org.h2.value.Transfer.readValue

package h2go

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReadValue reads a single value from the transfer stream based on the type info.
// The result is one of the permitted driver.Value types:
//   - nil (for NULL)
//   - int64 (for all integer types)
//   - float64 (for REAL/DOUBLE)
//   - string (for VARCHAR, CHAR, NUMERIC, etc.)
//   - []byte (for BINARY, VARBINARY)
//   - time.Time (for DATE, TIME, TIMESTAMP, and timezone variants)
//
// The colType parameter provides column-level type metadata used for recursive
// decoding of complex types such as ARRAY (where ExtTypeInfo holds the element
// type). For simple types the parameter is not needed.
//
// Standalone decoding: a fetch-on-demand LOB is fetched immediately via
// LOB_READ requests. Row readers must instead use readValueInternal with a
// lobCollector so deferred LOBs are transferred at the batch boundary (see
// Rows.fetchRows).
func (tr *Tr) ReadValue(colType *TypeInfo) (driver.Value, error) {
	return tr.readValueInternal(colType, nil)
}

// readValueInternal decodes a single value like ReadValue, but routes
// fetch-on-demand LOBs through lc when one is supplied: instead of issuing
// mid-batch LOB_READ requests (which desynchronizes the stream, because the
// server services operations only after finishing the in-flight batch), a
// placeholder is recorded in the collector and resolved by the caller at the
// batch boundary. A nil lc fetches immediately.
func (tr *Tr) readValueInternal(colType *TypeInfo, lc *lobCollector) (driver.Value, error) {
	// Read the value type code (this is the ValueType constant, not TI)
	typeCode, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: ReadValue: failed to read type code: %w", err)
	}

	switch typeCode {
	case ValueTypeNull:
		return nil, nil

	case ValueTypeBoolean:
		return tr.readBooleanValue()

	case ValueTypeTinyint:
		return tr.readTinyintValue()

	case ValueTypeSmallint:
		return tr.readSmallintValue()

	case ValueTypeInteger:
		return tr.readIntegerValue()

	case ValueTypeBigint:
		return tr.readBigintValue()

	case ValueTypeReal:
		return tr.readRealValue()

	case ValueTypeDouble:
		return tr.readDoubleValue()

	case ValueTypeNumeric:
		return tr.readNumericValue()

	case ValueTypeDate:
		return tr.readDateValue()

	case ValueTypeTime:
		return tr.readTimeValue()

	case ValueTypeTimeTZ:
		return tr.readTimeTZValue()

	case ValueTypeTimestamp:
		return tr.readTimestampValue()

	case ValueTypeTimestampTZ:
		return tr.readTimestampTZValue()

	case ValueTypeVarchar, ValueTypeChar, ValueTypeVarcharIgnoreCase:
		return tr.readStringValue()

	case ValueTypeVarbinary, ValueTypeBinary:
		return tr.readBytesValue()

	case ValueTypeUUID:
		return tr.readUUIDValue()

	case ValueTypeBlob, ValueTypeClob:
		// Inline LOB data is decoded directly; fetch-on-demand LOBs are either
		// deferred via lc or fetched immediately when lc is nil.
		return tr.readLOBValue(typeCode, lc)

	case ValueTypeDecfloat:
		// DECFLOAT is sent as a string
		return tr.readStringValue()

	case ValueTypeJSON, ValueTypeGeometry, ValueTypeJavaObject:
		return tr.readBytesValue()

	case ValueTypeEnum:
		return tr.readEnumValue()

	case ValueTypeInterval:
		return tr.readIntervalValue()

	case ValueTypeArray:
		return tr.readArrayValue(colType)

	case ValueTypeRow:
		return tr.readRowValue(colType)

	default:
		return nil, fmt.Errorf("h2go: ReadValue: %w: unknown type code %d", ErrUnsupportedType, typeCode)
	}
}

// readBooleanValue reads a BOOLEAN value.
func (tr *Tr) readBooleanValue() (driver.Value, error) {
	v, err := tr.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("h2go: readBooleanValue: %w", err)
	}
	return v, nil
}

// readTinyintValue reads a TINYINT value (1 byte, signed).
func (tr *Tr) readTinyintValue() (driver.Value, error) {
	b, err := tr.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTinyintValue: %w", err)
	}
	// Convert int8 to int64
	return int64(int8(b)), nil
}

// readSmallintValue reads a SMALLINT value (2 bytes, signed, big-endian).
// Protocol 20+ uses readShort (int16); older protocols used readInt cast to short.
func (tr *Tr) readSmallintValue() (driver.Value, error) {
	// Protocol 21 uses readShort (int16)
	v, err := tr.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("h2go: readSmallintValue: %w", err)
	}
	return int64(v), nil
}

// readIntegerValue reads an INTEGER value (4 bytes, signed, big-endian).
func (tr *Tr) readIntegerValue() (driver.Value, error) {
	v, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readIntegerValue: %w", err)
	}
	return int64(v), nil
}

// readBigintValue reads a BIGINT value (8 bytes, signed, big-endian).
func (tr *Tr) readBigintValue() (driver.Value, error) {
	v, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readBigintValue: %w", err)
	}
	return v, nil
}

// readRealValue reads a REAL value (4 bytes, IEEE 754 float32).
func (tr *Tr) readRealValue() (driver.Value, error) {
	v, err := tr.ReadFloat32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readRealValue: %w", err)
	}
	return float64(v), nil
}

// readDoubleValue reads a DOUBLE value (8 bytes, IEEE 754 float64).
func (tr *Tr) readDoubleValue() (driver.Value, error) {
	v, err := tr.ReadFloat64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readDoubleValue: %w", err)
	}
	return v, nil
}

// readNumericValue reads a NUMERIC value as a string.
// H2 sends NUMERIC as a string representation.
func (tr *Tr) readNumericValue() (driver.Value, error) {
	s, err := tr.ReadString()
	if err != nil {
		return nil, fmt.Errorf("h2go: readNumericValue: %w", err)
	}
	if s == nil {
		return nil, nil
	}
	return *s, nil
}

// readStringValue reads a VARCHAR/CHAR/VARCHAR_IGNORECASE value.
func (tr *Tr) readStringValue() (driver.Value, error) {
	s, err := tr.ReadString()
	if err != nil {
		return nil, fmt.Errorf("h2go: readStringValue: %w", err)
	}
	if s == nil {
		return nil, nil
	}
	return *s, nil
}

// readEnumValue reads an ENUM value (ordinal int32, protocol 21).
func (tr *Tr) readEnumValue() (driver.Value, error) {
	ordinal, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readEnumValue: %w", err)
	}
	return int64(ordinal), nil
}

// readIntervalValue reads an INTERVAL value and returns a human-readable string.
// Wire format: ordinal byte (negated when negative), then leading long, then
// optional remaining long for fractional-second interval types.
// Reference: Transfer.writeValue INTERVAL case in H2 2.4.240.
func (tr *Tr) readIntervalValue() (driver.Value, error) {
	x, err := tr.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("h2go: readIntervalValue: failed to read ordinal: %w", err)
	}
	// Byte is unsigned (0..255). When negative in Java (readByte signed),
	// x >= 128 means the java value < 0 → bit 7 is negation flag.
	negative := x >= 128
	var qualifierOrdinal int
	if negative {
		qualifierOrdinal = int(^x) & 0x7f
	} else {
		qualifierOrdinal = int(x)
	}

	qualifier := intervalQualifier(qualifierOrdinal)

	leading, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readIntervalValue: failed to read leading: %w", err)
	}
	if negative {
		leading = -leading
	}

	if qualifier.hasRemaining {
		remaining, err := tr.ReadInt64()
		if err != nil {
			return nil, fmt.Errorf("h2go: readIntervalValue: failed to read remaining: %w", err)
		}
		return formatInterval(qualifier.name, leading, remaining), nil
	}
	return formatInterval(qualifier.name, leading, 0), nil
}

// intervalQual holds the name and whether the interval type has a trailing
// remaining field (ordinal >= 5 in H2, which includes YEAR TO MONTH, DAY TO
// HOUR, etc., not just SECONDS).
type intervalQual struct {
	name         string
	hasRemaining bool
}

// intervalQualifier returns the interval qualifier metadata for the ordinal.
func intervalQualifier(ordinal int) intervalQual {
	switch ordinal {
	case 0:
		return intervalQual{"YEAR", false}
	case 1:
		return intervalQual{"MONTH", false}
	case 2:
		return intervalQual{"DAY", false}
	case 3:
		return intervalQual{"HOUR", false}
	case 4:
		return intervalQual{"MINUTE", false}
	case 5:
		return intervalQual{"SECOND", true}
	case 6:
		return intervalQual{"YEAR TO MONTH", true}
	case 7:
		return intervalQual{"DAY TO HOUR", true}
	case 8:
		return intervalQual{"DAY TO MINUTE", true}
	case 9:
		return intervalQual{"DAY TO SECOND", true}
	case 10:
		return intervalQual{"HOUR TO MINUTE", true}
	case 11:
		return intervalQual{"HOUR TO SECOND", true}
	case 12:
		return intervalQual{"MINUTE TO SECOND", true}
	default:
		return intervalQual{"UNKNOWN", false}
	}
}

// formatInterval renders an interval value exactly like H2's canonical
// interval text (IntervalUtils.appendInterval in combination with
// DateTimeUtils.appendNanos, verified against CAST(... AS VARCHAR) on live
// H2 2.4.240):
//   - the sign, when present, prefixes the leading field only;
//   - leading fields are never zero-padded ('2:03', '1-6', '100:03:04.5');
//   - sub-fields of compound qualifiers are two-digit padded ('2 03');
//   - fractional seconds use up to nine digits with trailing zeros trimmed;
//     an integral seconds value has no fraction at all ('7', '1 02:03:04').
//
// Units per qualifier follow ValueInterval/Transfer.writeValue: SECOND leads
// seconds with nanos remaining; DAY TO HOUR/MINUTE carry plain hours/minutes
// as remaining; every * TO SECOND carries the whole time part in nanos.
func formatInterval(qualifier string, leading, remaining int64) string {
	neg := ""
	if leading < 0 {
		neg = "-"
		leading = -leading
	}
	switch qualifier {
	case "YEAR", "MONTH", "DAY", "HOUR", "MINUTE":
		return fmt.Sprintf("INTERVAL '%s%d' %s", neg, leading, qualifier)
	case "SECOND":
		return fmt.Sprintf("INTERVAL '%s%d%s' SECOND", neg, leading, formatIntervalNanos(remaining))
	case "YEAR TO MONTH":
		return fmt.Sprintf("INTERVAL '%s%d-%d' YEAR TO MONTH", neg, leading, remaining)
	case "DAY TO HOUR":
		return fmt.Sprintf("INTERVAL '%s%d %02d' DAY TO HOUR", neg, leading, remaining)
	case "DAY TO MINUTE":
		return fmt.Sprintf("INTERVAL '%s%d %02d:%02d' DAY TO MINUTE", neg, leading, remaining/60, remaining%60)
	case "DAY TO SECOND":
		minutes := remaining / intervalNanosPerMinute
		nanos := remaining % intervalNanosPerMinute
		return fmt.Sprintf("INTERVAL '%s%d %02d:%02d:%02d%s' DAY TO SECOND",
			neg, leading, minutes/60, minutes%60,
			nanos/intervalNanosPerSecond, formatIntervalNanos(nanos%intervalNanosPerSecond))
	case "HOUR TO MINUTE":
		return fmt.Sprintf("INTERVAL '%s%d:%02d' HOUR TO MINUTE", neg, leading, remaining)
	case "HOUR TO SECOND":
		minutes := remaining / intervalNanosPerMinute
		secPart := remaining % intervalNanosPerMinute
		return fmt.Sprintf("INTERVAL '%s%d:%02d:%02d%s' HOUR TO SECOND",
			neg, leading, minutes, secPart/intervalNanosPerSecond,
			formatIntervalNanos(secPart%intervalNanosPerSecond))
	case "MINUTE TO SECOND":
		return fmt.Sprintf("INTERVAL '%s%d:%02d%s' MINUTE TO SECOND",
			neg, leading, remaining/intervalNanosPerSecond,
			formatIntervalNanos(remaining%intervalNanosPerSecond))
	default:
		return fmt.Sprintf("INTERVAL '%s%d' %s", neg, leading, qualifier)
	}
}

// Sub-field units used by formatInterval (NANOS_PER_SECOND / NANOS_PER_MINUTE
// in IntervalUtils.java).
const (
	intervalNanosPerSecond = int64(1_000_000_000)
	intervalNanosPerMinute = 60 * intervalNanosPerSecond
)

// formatIntervalNanos renders a sub-second nanos value the way
// DateTimeUtils.appendNanos does for canonical interval text: nothing at all
// when it is zero, otherwise a dot followed by nine digits with trailing zeros
// trimmed ('.5', '.25', '.000000001').
func formatIntervalNanos(nanos int64) string {
	if nanos <= 0 {
		return ""
	}
	return "." + strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
}

// nestedLOBs is passed while decoding values nested inside ARRAY/ROW
// containers. A fetch-on-demand LOB cannot be deferred there (the container
// value is rendered eagerly and would embed a placeholder in its text), nor
// fetched mid-batch (that would desynchronize the stream), so it is rejected
// with a documented ErrUnsupportedType error.
var nestedLOBs = &lobCollector{reject: true}

// errNestedOnDemandLOB marks a fetch-on-demand LOB encountered while decoding
// an ARRAY/ROW container element. It is wrapped together with ErrUnsupportedType
// at the raise site and checked by row readers to abort the session: the
// surrounding batch is left partially unconsumed on the wire, so the connection
// must not return to the pool.
var errNestedOnDemandLOB = errors.New("fetch-on-demand LOB inside ARRAY/ROW")

// pendingLob records a fetch-on-demand LOB (wire length == -1) whose transfer
// was deferred to the batch boundary. The frame fields match Transfer.readValue
// in H2 2.4.240 exactly.
type pendingLob struct {
	typeCode    int32  // ValueTypeBlob or ValueTypeClob
	lobID       int64  // server-side LOB identifier
	hmac        []byte // MAC authenticating the LOB_READ request
	octetLength int64  // CLOB only (protocol 20+): octet length; 0 for BLOB
	precision   int64  // BLOB: precision in octets; CLOB: character length
	row         int    // index of the row within the current batch buffer
	col         int    // index of the column within the row
}

// lobCollector accumulates deferred fetch-on-demand LOBs while a result batch
// or generated-keys frame is parsed. curRow/curCol are set by the row reader so
// each recorded placeholder knows where its resolved value belongs.
type lobCollector struct {
	pending []*pendingLob
	reject  bool // reject on-demand LOBs instead of recording them (ARRAY/ROW)
	curRow  int
	curCol  int
}

// readArrayValue reads an ARRAY value as a JSON-like string.
// Each element is decoded recursively using the element type from
// colType.ExtTypeInfo when available. Elements decode in "nested" mode: a
// fetch-on-demand LOB inside an ARRAY is rejected with ErrUnsupportedType
// (documented limitation), because the container value is rendered eagerly and
// deferring would embed a placeholder in the rendered text.
func (tr *Tr) readArrayValue(colType *TypeInfo) (driver.Value, error) {
	length, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readArrayValue: failed to read length: %w", err)
	}
	if length < 0 {
		// H2 1.4.200 and older may use negative length with a type name string
		length = ^length
		_, _ = tr.ReadString() // skip type name
	}
	if length == 0 {
		return "[]", nil
	}
	var elemType *TypeInfo
	if colType != nil {
		elemType = colType.ExtTypeInfo
	}
	elems := make([]string, 0, length)
	for i := int32(0); i < length; i++ {
		elem, err := tr.readValueInternal(elemType, nestedLOBs)
		if err != nil {
			return nil, fmt.Errorf("h2go: readArrayValue: element %d: %w", i, err)
		}
		elems = append(elems, fmt.Sprintf("%v", elem))
	}
	return "[" + strings.Join(elems, ",") + "]", nil
}

// readRowValue reads a ROW value as a JSON-like string.
// The colType parameter is accepted for signature symmetry with
// readArrayValue, but for MVP each field is decoded without element type
// hints (a future version could use ExtTypeInfo to look up field types).
func (tr *Tr) readRowValue(_ *TypeInfo) (driver.Value, error) {
	length, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readRowValue: failed to read length: %w", err)
	}
	fields := make([]string, 0, length)
	for i := int32(0); i < length; i++ {
		// For ROW with type info, we could look up the field type, but for MVP
		// we decode each field without element type hints. Fields decode in
		// "nested" mode like ARRAY elements (see readArrayValue).
		f, err := tr.readValueInternal(nil, nestedLOBs)
		if err != nil {
			return nil, fmt.Errorf("h2go: readRowValue: field %d: %w", i, err)
		}
		fields = append(fields, fmt.Sprintf("%v", f))
	}
	return "(" + strings.Join(fields, ",") + ")", nil
}

// readBytesValue reads a VARBINARY/BINARY value.
func (tr *Tr) readBytesValue() (driver.Value, error) {
	b, err := tr.ReadBytes()
	if err != nil {
		return nil, fmt.Errorf("h2go: readBytesValue: %w", err)
	}
	return b, nil
}

// readUUIDValue reads a UUID value (two int64: high bits, low bits).
// Returns the canonical string representation.
func (tr *Tr) readUUIDValue() (driver.Value, error) {
	high, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readUUIDValue: failed to read high: %w", err)
	}
	low, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readUUIDValue: failed to read low: %w", err)
	}
	return formatUUID(high, low), nil
}

// formatUUID formats high and low int64 into a canonical UUID string.
func formatUUID(high, low int64) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(high>>32),
		uint16(high>>16),
		uint16(high),
		uint16(low>>48),
		uint64(low)&0x0000FFFFFFFFFFFF)
}

// readLOBValue reads a BLOB or CLOB value.
// Inline LOBs (length >= 0) are decoded directly from the value stream.
// Fetch-on-demand LOBs (length == -1) are either recorded in lc for deferred
// transfer at the batch boundary, fetched immediately (lc == nil), or rejected
// when they appear nested inside an ARRAY/ROW container (lc == nestedLOBs).
func (tr *Tr) readLOBValue(typeCode int32, lc *lobCollector) (driver.Value, error) {
	length, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readLOBValue: failed to read length: %w", err)
	}

	// Fetch-on-demand LOB. Wire layout (Transfer.readValue, H2 2.4.240):
	//   BLOB: tableId int . lobId long . hmac bytes . precision long
	//   CLOB: tableId int . lobId long . hmac bytes . octetLength long . charLength long
	if length == -1 {
		tableID, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: readLOBValue: failed to read tableID: %w", err)
		}
		p := &pendingLob{
			typeCode: typeCode,
		}
		if lc != nil {
			p.row, p.col = lc.curRow, lc.curCol
		}
		p.lobID, err = tr.ReadInt64()
		if err != nil {
			return nil, fmt.Errorf("h2go: readLOBValue: failed to read lobID: %w", err)
		}
		p.hmac, err = tr.ReadBytes()
		if err != nil {
			return nil, fmt.Errorf("h2go: readLOBValue: failed to read hmac: %w", err)
		}
		if typeCode == ValueTypeClob {
			// Protocol 20+ sends both lengths for CLOB.
			p.octetLength, err = tr.ReadInt64()
			if err != nil {
				return nil, fmt.Errorf("h2go: readLOBValue: failed to read octetLength: %w", err)
			}
			p.precision, err = tr.ReadInt64() // character length
			if err != nil {
				return nil, fmt.Errorf("h2go: readLOBValue: failed to read charLength: %w", err)
			}
		} else {
			// BLOB frames carry only the precision (octet length).
			p.precision, err = tr.ReadInt64()
			if err != nil {
				return nil, fmt.Errorf("h2go: readLOBValue: failed to read precision: %w", err)
			}
		}
		_ = tableID // not needed for server-side lookup; lobID is the key

		switch {
		case lc == nil:
			// Standalone decode: fetch immediately (legacy behaviour).
			return tr.fetchLob(p)
		case lc.reject:
			return nil, fmt.Errorf("h2go: ReadValue: %w: %w is not supported", ErrUnsupportedType, errNestedOnDemandLOB)
		default:
			lc.pending = append(lc.pending, p)
			return p, nil
		}
	}

	if length < 0 {
		return nil, fmt.Errorf("h2go: readLOBValue: invalid LOB length %d", length)
	}

	// For BLOB: read raw bytes
	if typeCode == ValueTypeBlob {
		data := make([]byte, length)
		if err := tr.ReadFull(data); err != nil {
			return nil, fmt.Errorf("h2go: readLOBValue: failed to read BLOB data: %w", err)
		}
		// Read and verify magic number
		magic, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: readLOBValue: failed to read BLOB magic: %w", err)
		}
		if magic != lobMagic {
			return nil, fmt.Errorf("h2go: readLOBValue: invalid BLOB magic %d", magic)
		}
		return data, nil
	}

	// For CLOB: read `length` UTF-8 code points as written by Data.copyString.
	if typeCode == ValueTypeClob {
		return tr.readInlineClob(length)
	}

	return nil, fmt.Errorf("h2go: readLOBValue: %w: type code %d", ErrUnsupportedType, typeCode)
}

// lobReadChunkSize is the maximum bytes requested per LOB_READ operation.
// Matches the server's limit of 16 * IO_BUFFER_SIZE (64 KiB).
const lobReadChunkSize = 16 * 4096

// fetchLob transfers a deferred fetch-on-demand LOB by issuing repeated
// LOB_READ requests until all data has been received.
// It must only be called at a batch boundary — when no partially parsed result
// batch is in flight — because the server services operations sequentially
// after finishing the in-flight sendRows (see Rows.fetchRows).
func (tr *Tr) fetchLob(p *pendingLob) (driver.Value, error) {
	var totalBytes int64
	if p.typeCode == ValueTypeBlob {
		totalBytes = p.precision // BLOB precision counts octets
	} else {
		totalBytes = p.octetLength // CLOB octet length; 0 means unknown size
	}
	if totalBytes > MaxWireLength {
		return nil, fmt.Errorf("h2go: fetchLob: LOB length %d exceeds cap %d", totalBytes, MaxWireLength)
	}

	var buf bytes.Buffer
	if totalBytes > 0 {
		buf.Grow(int(totalBytes))
	}

	var offset int64
	for {
		if err := tr.WriteInt32(LobRead); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: write op: %w", err)
		}
		if err := tr.WriteInt64(p.lobID); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: write lobID: %w", err)
		}
		if err := tr.WriteBytes(p.hmac); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: write hmac: %w", err)
		}
		if err := tr.WriteInt64(offset); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: write offset: %w", err)
		}
		if err := tr.WriteInt32(lobReadChunkSize); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: write length: %w", err)
		}
		if err := tr.Flush(); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: flush: %w", err)
		}

		status, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: read status: %w", err)
		}
		if status != StatusOK {
			return nil, fmt.Errorf("h2go: fetchLob: unexpected status %d", status)
		}

		actualLen, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: read actual length: %w", err)
		}
		if actualLen <= 0 {
			break // end of LOB data
		}

		chunk := make([]byte, actualLen)
		if err := tr.ReadFull(chunk); err != nil {
			return nil, fmt.Errorf("h2go: fetchLob: read chunk: %w", err)
		}
		buf.Write(chunk)
		offset += int64(actualLen)
	}

	if p.typeCode == ValueTypeClob {
		// CLOB payload is UTF-8 encoded
		return buf.String(), nil
	}
	return buf.Bytes(), nil
}

// lobMagic is the magic number written by H2 after inline LOB data.
// Reference: Transfer.LOB_MAGIC = 0x1234 in H2 2.4.240.
const lobMagic = 0x1234

// maxInlineClobChars caps the character length the driver will accept for an
// inline CLOB before allocating a decode buffer. It is a DoS guard, not a
// semantic limit: H2's MAX_LENGTH_INPLACE_LOB is far below this value in any
// realistic deployment.
const maxInlineClobChars = 1 << 28 // 268M chars

// readInlineClob reads `length` UTF-8 code points from the stream and verifies
// the trailing LOB_MAGIC. Encoding matches Data.copyString / DataReader.readChar:
// 1-byte for <0x80, 2-byte for 0x80..0x7FF (first byte 0xC0|..), 3-byte for
// >=0x800 (first byte 0xE0|..). Note H2 writes chars (UTF-16 code units), so
// supplementary characters arrive as a surrogate pair of 3-byte sequences.
func (tr *Tr) readInlineClob(length int64) (driver.Value, error) {
	if length > maxInlineClobChars {
		return nil, fmt.Errorf("h2go: readInlineClob: CLOB char length %d exceeds cap %d", length, maxInlineClobChars)
	}
	var sb strings.Builder
	if length > 0 {
		sb.Grow(int(length)) // bytes >= chars, so this avoids most reallocations
	}
	for i := int64(0); i < length; i++ {
		c, err := tr.readClobChar()
		if err != nil {
			return nil, fmt.Errorf("h2go: readInlineClob: failed to read char %d of %d: %w", i, length, err)
		}
		sb.WriteRune(c)
	}
	magic, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readInlineClob: failed to read CLOB magic: %w", err)
	}
	if magic != lobMagic {
		return nil, fmt.Errorf("h2go: readInlineClob: invalid CLOB magic %d", magic)
	}
	return sb.String(), nil
}

// readClobChar reads one character using the DataReader.readChar encoding.
func (tr *Tr) readClobChar() (rune, error) {
	x, err := tr.ReadByte()
	if err != nil {
		return 0, err
	}
	if x < 0x80 {
		return rune(x), nil
	}
	if x >= 0xe0 {
		b1, err := tr.ReadByte()
		if err != nil {
			return 0, err
		}
		b2, err := tr.ReadByte()
		if err != nil {
			return 0, err
		}
		return rune(int(x&0x0f)<<12 | int(b1&0x3f)<<6 | int(b2&0x3f)), nil
	}
	b1, err := tr.ReadByte()
	if err != nil {
		return 0, err
	}
	return rune(int(x&0x1f)<<6 | int(b1&0x3f)), nil
}

// readDateValue reads a DATE value.
// H2 stores dates as a "dateValue" (days since 1970-01-01, proleptic Gregorian).
func (tr *Tr) readDateValue() (driver.Value, error) {
	dateValue, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readDateValue: %w", err)
	}
	return dateValueToTime(dateValue), nil
}

// readTimeValue reads a TIME value.
// H2 stores time as nanoseconds since midnight.
func (tr *Tr) readTimeValue() (driver.Value, error) {
	nanos, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimeValue: %w", err)
	}
	return nanosOfDayToTime(nanos), nil
}

// readTimeTZValue reads a TIME WITH TIME ZONE value.
// Format: nanoseconds + timezone offset in seconds.
func (tr *Tr) readTimeTZValue() (driver.Value, error) {
	nanos, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimeTZValue: failed to read nanos: %w", err)
	}
	offsetSec, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimeTZValue: failed to read offset: %w", err)
	}
	return nanosOfDayToTimeTZ(nanos, int(offsetSec)), nil
}

// readTimestampValue reads a TIMESTAMP value.
// Format: dateValue (days since epoch) + nanoseconds of day.
func (tr *Tr) readTimestampValue() (driver.Value, error) {
	dateValue, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimestampValue: failed to read dateValue: %w", err)
	}
	nanos, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimestampValue: failed to read nanos: %w", err)
	}
	return timestampToTime(dateValue, nanos), nil
}

// readTimestampTZValue reads a TIMESTAMP WITH TIME ZONE value.
// Format: dateValue + nanoseconds + timezone offset in seconds.
func (tr *Tr) readTimestampTZValue() (driver.Value, error) {
	dateValue, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimestampTZValue: failed to read dateValue: %w", err)
	}
	nanos, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimestampTZValue: failed to read nanos: %w", err)
	}
	offsetSec, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: readTimestampTZValue: failed to read offset: %w", err)
	}
	return timestampToTimeTZ(dateValue, nanos, int(offsetSec)), nil
}

// Date/time conversion utilities
//
// H2 encodes dates as a packed "dateValue" using:
//
//	dateValue = (year << SHIFT_YEAR) | (month << SHIFT_MONTH) | day
//	         = (year << 9)          | (month << 5)           | day
//
// where SHIFT_YEAR=9, SHIFT_MONTH=5 (DateTimeUtils.java in H2 2.4.240).
// This is NOT days since the Unix epoch.
//
// For TIMESTAMP, dateValue+timeNanos both represent the UTC instant.
// For TIMESTAMP WITH TIME ZONE and TIME WITH TIME ZONE, dateValue+timeNanos
// represent the LOCAL time in the given timezone; the offsetSec converts to UTC.

// unpackDateValue extracts year, month, and day from H2's packed date value.
func unpackDateValue(dateValue int64) (year int, month time.Month, day int) {
	day = int(dateValue & 0x1F)
	month = time.Month((dateValue >> 5) & 0x0F)
	year = int(dateValue >> 9)
	return
}

// dateValueToTime converts an H2 packed dateValue to a time.Time (UTC, midnight).
func dateValueToTime(dateValue int64) time.Time {
	y, m, d := unpackDateValue(dateValue)
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// nanosOfDayToTime converts nanoseconds since midnight to a time.Time
// using a zero date (year 0000, Jan 1, UTC).
func nanosOfDayToTime(nanos int64) time.Time {
	hour := nanos / (60 * 60 * 1e9)
	nanos %= 60 * 60 * 1e9
	minute := nanos / (60 * 1e9)
	nanos %= 60 * 1e9
	sec := nanos / 1e9
	nsec := nanos % 1e9
	return time.Date(0, 1, 1, int(hour), int(minute), int(sec), int(nsec), time.UTC)
}

// nanosOfDayToTimeTZ converts nanoseconds since midnight (LOCAL time) and a UTC
// offset to a time.Time in the given fixed zone.
func nanosOfDayToTimeTZ(nanos int64, offsetSec int) time.Time {
	loc := time.FixedZone("", offsetSec)
	hour := nanos / (60 * 60 * 1e9)
	nanos %= 60 * 60 * 1e9
	minute := nanos / (60 * 1e9)
	nanos %= 60 * 1e9
	sec := nanos / 1e9
	nsec := nanos % 1e9
	return time.Date(0, 1, 1, int(hour), int(minute), int(sec), int(nsec), loc)
}

// timestampToTime converts H2 dateValue + nanoseconds-of-day to a UTC time.Time.
// Both values represent UTC.
func timestampToTime(dateValue, nanos int64) time.Time {
	y, m, d := unpackDateValue(dateValue)
	hour := nanos / (60 * 60 * 1e9)
	nanos %= 60 * 60 * 1e9
	minute := nanos / (60 * 1e9)
	nanos %= 60 * 1e9
	sec := nanos / 1e9
	nsec := nanos % 1e9
	return time.Date(y, m, d, int(hour), int(minute), int(sec), int(nsec), time.UTC)
}

// timestampToTimeTZ converts H2 dateValue + nanoseconds-of-day (LOCAL) and a UTC
// offset to a time.Time in the given fixed zone.
// The dateValue and nanos represent LOCAL time in the timezone given by offsetSec.
func timestampToTimeTZ(dateValue, nanos int64, offsetSec int) time.Time {
	loc := time.FixedZone("", offsetSec)
	y, m, d := unpackDateValue(dateValue)
	hour := nanos / (60 * 60 * 1e9)
	nanos %= 60 * 60 * 1e9
	minute := nanos / (60 * 1e9)
	nanos %= 60 * 1e9
	sec := nanos / 1e9
	nsec := nanos % 1e9
	return time.Date(y, m, d, int(hour), int(minute), int(sec), int(nsec), loc)
}

// valueTypeName returns a human-readable name for a ValueType constant.
func valueTypeName(vt int32) string {
	switch vt {
	case ValueTypeNull:
		return "NULL"
	case ValueTypeBoolean:
		return "BOOLEAN"
	case ValueTypeTinyint:
		return "TINYINT"
	case ValueTypeSmallint:
		return "SMALLINT"
	case ValueTypeInteger:
		return "INTEGER"
	case ValueTypeBigint:
		return "BIGINT"
	case ValueTypeNumeric:
		return "NUMERIC"
	case ValueTypeDouble:
		return "DOUBLE"
	case ValueTypeReal:
		return "REAL"
	case ValueTypeTime:
		return "TIME"
	case ValueTypeDate:
		return "DATE"
	case ValueTypeTimestamp:
		return "TIMESTAMP"
	case ValueTypeTimestampTZ:
		return "TIMESTAMP_TZ"
	case ValueTypeTimeTZ:
		return "TIME_TZ"
	case ValueTypeVarbinary:
		return "VARBINARY"
	case ValueTypeBinary:
		return "BINARY"
	case ValueTypeVarchar:
		return "VARCHAR"
	case ValueTypeVarcharIgnoreCase:
		return "VARCHAR_IGNORECASE"
	case ValueTypeChar:
		return "CHAR"
	case ValueTypeBlob:
		return "BLOB"
	case ValueTypeClob:
		return "CLOB"
	case ValueTypeArray:
		return "ARRAY"
	case ValueTypeJavaObject:
		return "JAVA_OBJECT"
	case ValueTypeUUID:
		return "UUID"
	case ValueTypeGeometry:
		return "GEOMETRY"
	case ValueTypeEnum:
		return "ENUM"
	case ValueTypeInterval:
		return "INTERVAL"
	case ValueTypeRow:
		return "ROW"
	case ValueTypeJSON:
		return "JSON"
	case ValueTypeDecfloat:
		return "DECFLOAT"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", vt)
	}
}
