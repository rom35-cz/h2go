// value_read.go — H2 wire value decoder (protocol 21).
//
// Reference: org.h2.value.Transfer.readValue

package h2go

import (
	"database/sql/driver"
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
// The _ parameter is the column type info which may be used in future for
// type-specific decoding (currently types are self-describing in wire format).
func (tr *Tr) ReadValue(_ *TypeInfo) (driver.Value, error) {
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
		// For MVP, we read the inline LOB data or return an error for fetch-on-demand
		return tr.readLOBValue(typeCode)

	case ValueTypeDecfloat:
		// DECFLOAT is sent as a string
		return tr.readStringValue()

	case ValueTypeArray, ValueTypeRow, ValueTypeEnum, ValueTypeGeometry,
		ValueTypeInterval, ValueTypeJavaObject, ValueTypeJSON:
		// MVP: return error for unsupported complex types
		return nil, fmt.Errorf("h2go: ReadValue: %w: type code %d (%s)",
			ErrUnsupportedType, typeCode, valueTypeName(typeCode))

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
// For MVP, this supports inline LOBs only (length >= 0).
// Fetch-on-demand LOBs (length == -1) return an error.
func (tr *Tr) readLOBValue(typeCode int32) (driver.Value, error) {
	length, err := tr.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("h2go: readLOBValue: failed to read length: %w", err)
	}

	// Fetch-on-demand LOB (not supported in MVP)
	if length == -1 {
		// Skip the fetch-on-demand metadata
		_, _ = tr.ReadInt32() // tableId
		_, _ = tr.ReadInt64() // id
		_, _ = tr.ReadBytes() // hmac
		if typeCode == ValueTypeClob {
			_, _ = tr.ReadInt64() // octetLength (protocol 20+)
		}
		_, _ = tr.ReadInt64() // charLength or precision
		return nil, fmt.Errorf("h2go: readLOBValue: %w: fetch-on-demand LOB", ErrUnsupportedType)
	}

	if length < 0 {
		return nil, fmt.Errorf("h2go: readLOBValue: invalid LOB length %d", length)
	}

	// For BLOB: read raw bytes
	if typeCode == ValueTypeBlob {
		// In H2 wire format, BLOB data is read via createBlob which reads from the stream
		// For inline BLOBs with known length, we read the bytes directly
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
