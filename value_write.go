// value_write.go — H2 wire value encoder (protocol 21).
//
// Reference: org.h2.value.Transfer.writeValue

package h2go

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WriteValue writes a single parameter value in H2 wire value format.
//
// Supported driver.Value inputs for MVP:
//   - nil
//   - bool
//   - int64
//   - float64
//   - string
//   - []byte
//   - time.Time
//
// When paramType is available (from prepared-parameter metadata), it is used
// to encode values more precisely where required by the PRD:
//   - string/int64/float64 + NUMERIC/DECFLOAT parameter -> numeric/decfloat wire value
//   - string + UUID parameter -> UUID wire value (high/low int64)
//   - time.Time + date/time/timestamp param -> matching temporal wire layout
func (tr *Tr) WriteValue(v driver.Value, paramType *TypeInfo) error {
	switch x := v.(type) {
	case nil:
		return tr.WriteInt32(ValueTypeNull)

	case bool:
		if err := tr.WriteInt32(ValueTypeBoolean); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BOOLEAN type: %w", err)
		}
		if err := tr.WriteBool(x); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BOOLEAN value: %w", err)
		}
		return nil

	case int64:
		if paramType != nil && (paramType.ValueType == ValueTypeNumeric || paramType.ValueType == ValueTypeDecfloat) {
			return tr.writeStringValue(strconv.FormatInt(x, 10), paramType)
		}
		if err := tr.WriteInt32(ValueTypeBigint); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BIGINT type: %w", err)
		}
		if err := tr.WriteInt64(x); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BIGINT value: %w", err)
		}
		return nil

	case float64:
		if paramType != nil && (paramType.ValueType == ValueTypeNumeric || paramType.ValueType == ValueTypeDecfloat) {
			return tr.writeStringValue(strconv.FormatFloat(x, 'g', -1, 64), paramType)
		}
		// Default float64 mapping is DOUBLE. If parameter metadata says REAL,
		// encode REAL to match server-side expectation.
		if paramType != nil && paramType.ValueType == ValueTypeReal {
			if err := tr.WriteInt32(ValueTypeReal); err != nil {
				return fmt.Errorf("h2go: WriteValue: failed to write REAL type: %w", err)
			}
			if err := tr.WriteFloat32(float32(x)); err != nil {
				return fmt.Errorf("h2go: WriteValue: failed to write REAL value: %w", err)
			}
			return nil
		}
		if err := tr.WriteInt32(ValueTypeDouble); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write DOUBLE type: %w", err)
		}
		if err := tr.WriteFloat64(x); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write DOUBLE value: %w", err)
		}
		return nil

	case string:
		return tr.writeStringValue(x, paramType)

	case []byte:
		if x == nil {
			return tr.WriteInt32(ValueTypeNull)
		}
		valueType := int32(ValueTypeVarbinary)
		if paramType != nil && paramType.ValueType == ValueTypeBinary {
			valueType = ValueTypeBinary
		}
		if err := tr.WriteInt32(valueType); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BINARY/VARBINARY type: %w", err)
		}
		if err := tr.WriteBytes(x); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write BINARY/VARBINARY value: %w", err)
		}
		return nil

	case time.Time:
		return tr.writeTimeValue(x, paramType)

	default:
		return fmt.Errorf("h2go: WriteValue: %w: unsupported driver.Value type %T", ErrUnsupportedType, v)
	}
}

func (tr *Tr) writeStringValue(s string, paramType *TypeInfo) error {
	valueType := int32(ValueTypeVarchar)
	if paramType != nil {
		switch paramType.ValueType {
		case ValueTypeNumeric:
			valueType = ValueTypeNumeric
		case ValueTypeDecfloat:
			valueType = ValueTypeDecfloat
		case ValueTypeChar:
			valueType = ValueTypeChar
		case ValueTypeVarcharIgnoreCase:
			valueType = ValueTypeVarcharIgnoreCase
		case ValueTypeUUID:
			high, low, err := parseUUIDString(s)
			if err != nil {
				return fmt.Errorf("h2go: WriteValue: invalid UUID %q: %w", s, err)
			}
			if err := tr.WriteInt32(ValueTypeUUID); err != nil {
				return fmt.Errorf("h2go: WriteValue: failed to write UUID type: %w", err)
			}
			if err := tr.WriteInt64(high); err != nil {
				return fmt.Errorf("h2go: WriteValue: failed to write UUID high: %w", err)
			}
			if err := tr.WriteInt64(low); err != nil {
				return fmt.Errorf("h2go: WriteValue: failed to write UUID low: %w", err)
			}
			return nil
		}
	}
	if err := tr.WriteInt32(valueType); err != nil {
		return fmt.Errorf("h2go: WriteValue: failed to write string type: %w", err)
	}
	if err := tr.WriteString(s); err != nil {
		return fmt.Errorf("h2go: WriteValue: failed to write string value: %w", err)
	}
	return nil
}

func (tr *Tr) writeTimeValue(ts time.Time, paramType *TypeInfo) error {
	valueType := ValueTypeTimestamp
	if paramType != nil {
		switch paramType.ValueType {
		case ValueTypeDate, ValueTypeTime, ValueTypeTimeTZ, ValueTypeTimestamp, ValueTypeTimestampTZ:
			valueType = paramType.ValueType
		}
	}

	y, m, d := ts.Date()
	dateValue := packDateValue(y, m, d)
	nanos := nanosOfDay(ts)
	_, offset := ts.Zone()

	switch valueType {
	case ValueTypeDate:
		if err := tr.WriteInt32(ValueTypeDate); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write DATE type: %w", err)
		}
		if err := tr.WriteInt64(dateValue); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write DATE value: %w", err)
		}
		return nil

	case ValueTypeTime:
		if err := tr.WriteInt32(ValueTypeTime); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIME type: %w", err)
		}
		if err := tr.WriteInt64(nanos); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIME value: %w", err)
		}
		return nil

	case ValueTypeTimeTZ:
		if err := tr.WriteInt32(ValueTypeTimeTZ); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIME_TZ type: %w", err)
		}
		if err := tr.WriteInt64(nanos); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIME_TZ nanos: %w", err)
		}
		if err := tr.WriteInt32(int32(offset)); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIME_TZ offset: %w", err)
		}
		return nil

	case ValueTypeTimestampTZ:
		if err := tr.WriteInt32(ValueTypeTimestampTZ); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP_TZ type: %w", err)
		}
		if err := tr.WriteInt64(dateValue); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP_TZ date: %w", err)
		}
		if err := tr.WriteInt64(nanos); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP_TZ nanos: %w", err)
		}
		if err := tr.WriteInt32(int32(offset)); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP_TZ offset: %w", err)
		}
		return nil

	default: // TIMESTAMP (also default when metadata is not available)
		if err := tr.WriteInt32(ValueTypeTimestamp); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP type: %w", err)
		}
		if err := tr.WriteInt64(dateValue); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP date: %w", err)
		}
		if err := tr.WriteInt64(nanos); err != nil {
			return fmt.Errorf("h2go: WriteValue: failed to write TIMESTAMP nanos: %w", err)
		}
		return nil
	}
}

// packDateValue packs year, month, day to H2's dateValue representation:
// (year << 9) | (month << 5) | day.
func packDateValue(year int, month time.Month, day int) int64 {
	return int64((year << 9) | (int(month) << 5) | day)
}

// nanosOfDay returns nanoseconds since midnight from the local wall-clock part
// of the supplied time value.
func nanosOfDay(ts time.Time) int64 {
	h, m, s := ts.Clock()
	return int64(h)*int64(time.Hour) +
		int64(m)*int64(time.Minute) +
		int64(s)*int64(time.Second) +
		int64(ts.Nanosecond())
}

// parseUUIDString parses canonical UUID text into high and low 64-bit values.
// Accepted forms:
//   - xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
//   - xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
func parseUUIDString(s string) (high, low int64, err error) {
	uuid := strings.TrimSpace(s)
	switch len(uuid) {
	case 36:
		if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
			return 0, 0, fmt.Errorf("expected canonical UUID hyphen positions")
		}
		uuid = strings.ReplaceAll(uuid, "-", "")
	case 32:
		// already compact hex form
	default:
		return 0, 0, fmt.Errorf("expected 32 or 36 characters, got %d", len(uuid))
	}

	h, err := strconv.ParseUint(uuid[:16], 16, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid UUID high bits: %w", err)
	}
	l, err := strconv.ParseUint(uuid[16:], 16, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid UUID low bits: %w", err)
	}
	return int64(h), int64(l), nil
}
