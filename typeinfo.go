// typeinfo.go — H2 TypeInfo protocol-21 codec.
//
// Reference: org.h2.value.TypeInfo, org.h2.value.Transfer.writeTypeInfo20/readTypeInfo20

package h2go

import (
	"fmt"
)

// TypeInfo represents H2 column/parameter type metadata.
// This is used for both result columns and prepared statement parameters.
type TypeInfo struct {
	// ValueType is the H2 ValueType constant (ValueTypeNull, ValueTypeInteger, etc.)
	ValueType int

	// Precision is the declared precision (max length for strings, digits for numerics).
	// -1 if not applicable or unknown.
	Precision int64

	// Scale is the declared scale (decimal places for numerics).
	// -1 if not applicable or unknown.
	Scale int

	// Nullable indicates nullability: 0 = unknown, 1 = not nullable, 2 = nullable.
	// Uses database/sql nullable constants.
	Nullable int

	// ExtTypeInfo holds extended type info for complex types (ENUM, GEOMETRY, ROW, ARRAY).
	// For ARRAY, this is the element TypeInfo.
	// For simple types, this is nil.
	ExtTypeInfo *TypeInfo
}

// Nullable constants matching java.sql.ResultSetMetaData.
const (
	ColumnNullableUnknown = 0
	ColumnNoNulls         = 1
	ColumnNullable        = 2
)

// TI (Type Information) type ID constants for protocol 20+.
// These are the on-wire type identifiers used in TypeInfo metadata frames,
// distinct from the ValueType codes used in value frames.
//
// Reference: Transfer.java static initialiser (addType calls) in H2 2.4.240.
// Note the gaps: TI code 18 and 23 are unused in H2.
const (
	TIUnknown           = -1
	TINull              = 0
	TIBoolean           = 1
	TITinyint           = 2
	TISmallint          = 3
	TIInteger           = 4
	TIBigint            = 5
	TINumeric           = 6
	TIDouble            = 7
	TIReal              = 8
	TITime              = 9
	TIDate              = 10
	TITimestamp         = 11
	TIVarbinary         = 12
	TIVarchar           = 13
	TIVarcharIgnoreCase = 14
	TIBlob              = 15
	TIClob              = 16
	TIArray             = 17
	// 18 is unused in H2's TI table.
	TIJavaObject = 19
	TIUUID       = 20
	TIChar       = 21
	TIGeometry   = 22
	// 23 is unused in H2's TI table.
	TITimestampTZ       = 24
	TIEnum              = 25
	TIIntervalYear      = 26
	TIIntervalMonth     = 27
	TIIntervalDay       = 28
	TIIntervalHour      = 29
	TIIntervalMinute    = 30
	TIIntervalSecond    = 31
	TIIntervalYearMonth = 32
	TIIntervalDayHour   = 33
	TIIntervalDayMinute = 34
	TIIntervalDaySecond = 35
	TIIntervalHourMin   = 36
	TIIntervalHourSec   = 37
	TIIntervalMinSec    = 38
	TIRow               = 39
	TIJSON              = 40
	TITimeTZ            = 41
	TIBinary            = 42
	TIDecfloat          = 43
)

// tiToValueType maps TI codes to ValueType constants.
// This is the inverse of VALUE_TO_TI in H2's Transfer class.
var tiToValueType = map[int]int{
	TIUnknown:           ValueTypeNull, // maps to NULL for unknown
	TINull:              ValueTypeNull,
	TIBoolean:           ValueTypeBoolean,
	TITinyint:           ValueTypeTinyint,
	TISmallint:          ValueTypeSmallint,
	TIInteger:           ValueTypeInteger,
	TIBigint:            ValueTypeBigint,
	TINumeric:           ValueTypeNumeric,
	TIDouble:            ValueTypeDouble,
	TIReal:              ValueTypeReal,
	TITime:              ValueTypeTime,
	TIDate:              ValueTypeDate,
	TITimestamp:         ValueTypeTimestamp,
	TIVarbinary:         ValueTypeVarbinary,
	TIVarchar:           ValueTypeVarchar,
	TIVarcharIgnoreCase: ValueTypeVarcharIgnoreCase,
	TIBlob:              ValueTypeBlob,
	TIClob:              ValueTypeClob,
	TIArray:             ValueTypeArray,
	TIJavaObject:        ValueTypeJavaObject,
	TIUUID:              ValueTypeUUID,
	TIChar:              ValueTypeChar,
	TIGeometry:          ValueTypeGeometry,
	TITimestampTZ:       ValueTypeTimestampTZ,
	TIEnum:              ValueTypeEnum,
	TIIntervalYear:      ValueTypeInterval,
	TIIntervalMonth:     ValueTypeInterval,
	TIIntervalDay:       ValueTypeInterval,
	TIIntervalHour:      ValueTypeInterval,
	TIIntervalMinute:    ValueTypeInterval,
	TIIntervalSecond:    ValueTypeInterval,
	TIIntervalYearMonth: ValueTypeInterval,
	TIIntervalDayHour:   ValueTypeInterval,
	TIIntervalDayMinute: ValueTypeInterval,
	TIIntervalDaySecond: ValueTypeInterval,
	TIIntervalHourMin:   ValueTypeInterval,
	TIIntervalHourSec:   ValueTypeInterval,
	TIIntervalMinSec:    ValueTypeInterval,
	TIRow:               ValueTypeRow,
	TIJSON:              ValueTypeJSON,
	TITimeTZ:            ValueTypeTimeTZ,
	TIBinary:            ValueTypeBinary,
	TIDecfloat:          ValueTypeDecfloat,
}

// ReadTypeInfo reads TypeInfo metadata from the transfer stream (protocol 20+).
// This must only be called after handshake has established version >= 20.
func (tr *Tr) ReadTypeInfo() (*TypeInfo, error) {
	// Read the TI type identifier
	ti, err := tr.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read type id: %w", err)
	}

	// Map TI to ValueType
	valueType, ok := tiToValueType[int(ti)]
	if !ok {
		// Unknown type code - treat as UNKNOWN
		valueType = ValueTypeNull
	}

	info := &TypeInfo{
		ValueType: valueType,
		Precision: -1,
		Scale:     -1,
		Nullable:  ColumnNullableUnknown,
	}

	// Read type-specific attributes based on ValueType
	switch valueType {
	// Simple types with no additional metadata
	case ValueTypeNull, ValueTypeBoolean, ValueTypeTinyint, ValueTypeSmallint,
		ValueTypeInteger, ValueTypeBigint, ValueTypeDate, ValueTypeUUID:
		// No additional fields

	// Types with precision only (as int32)
	case ValueTypeChar, ValueTypeVarchar, ValueTypeVarcharIgnoreCase,
		ValueTypeBinary, ValueTypeVarbinary, ValueTypeDecfloat, ValueTypeJavaObject,
		ValueTypeJSON:
		prec, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read precision: %w", err)
		}
		info.Precision = int64(prec)

	// LOB types with precision as int64
	case ValueTypeBlob, ValueTypeClob:
		prec, err := tr.ReadInt64()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read LOB precision: %w", err)
		}
		info.Precision = prec

	// NUMERIC with precision, scale, and extended info flag
	case ValueTypeNumeric:
		prec, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read NUMERIC precision: %w", err)
		}
		info.Precision = int64(prec)

		scale, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read NUMERIC scale: %w", err)
		}
		info.Scale = int(scale)

		// Read extended type info flag (hasExt boolean)
		hasExt, err := tr.ReadBool()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read NUMERIC ext flag: %w", err)
		}
		_ = hasExt // Extended type info not stored for MVP

	// Floating point and some interval types with byte precision
	case ValueTypeReal, ValueTypeDouble:
		precByte, err := tr.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read precision byte: %w", err)
		}
		if precByte != 0xFF {
			info.Precision = int64(int8(precByte))
		}

	// Time/timestamp types with byte scale
	case ValueTypeTime, ValueTypeTimeTZ, ValueTypeTimestamp, ValueTypeTimestampTZ:
		scaleByte, err := tr.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read scale byte: %w", err)
		}
		if scaleByte != 0xFF {
			info.Scale = int(int8(scaleByte))
		}

	// Interval types — leading precision byte always present;
	// trailing scale byte only for fractional-second variants.
	case ValueTypeInterval:
		// Read leading precision byte (always present for all interval types).
		precByte, err := tr.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read interval precision: %w", err)
		}
		if precByte != 0xFF {
			info.Precision = int64(int8(precByte))
		}
		// Trailing scale byte only for fractional-second interval types:
		// INTERVAL SECOND, DAY TO SECOND, HOUR TO SECOND, MINUTE TO SECOND.
		if intervalHasFractionalSeconds(int(ti)) {
			scaleByte, err := tr.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read interval scale: %w", err)
			}
			if scaleByte != 0xFF {
				info.Scale = int(int8(scaleByte))
			}
		}

	// ARRAY with element count and element type
	case ValueTypeArray:
		// Read declared precision (array length)
		prec, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read ARRAY precision: %w", err)
		}
		info.Precision = int64(prec)

		// Read element type info recursively
		elemType, err := tr.ReadTypeInfo()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadTypeInfo: failed to read ARRAY element type: %w", err)
		}
		info.ExtTypeInfo = elemType

	// ROW, ENUM, GEOMETRY - extended info (simplified for MVP)
	case ValueTypeRow, ValueTypeEnum, ValueTypeGeometry:
		// For MVP, we skip detailed extended type info parsing
		// ROW would need field name/type pairs
		// ENUM would need enumerator values
		// GEOMETRY would need type + SRID
		// These are read but not fully parsed
		if err := skipExtTypeInfo(tr, valueType); err != nil {
			return nil, err
		}

	default:
		// Unknown type - skip any potential additional data conservatively
	}

	return info, nil
}

// skipExtTypeInfo skips extended type info for complex types (MVP simplification).
func skipExtTypeInfo(tr *Tr, valueType int) error {
	switch valueType {
	case ValueTypeEnum:
		// Read count of enumerators, then each enumerator string
		count, err := tr.ReadInt32()
		if err != nil {
			return fmt.Errorf("h2go: skipExtTypeInfo: failed to read ENUM count: %w", err)
		}
		for i := 0; i < int(count); i++ {
			_, err := tr.ReadString()
			if err != nil {
				return fmt.Errorf("h2go: skipExtTypeInfo: failed to read ENUM value: %w", err)
			}
		}

	case ValueTypeGeometry:
		// Read type indicator byte
		typ, err := tr.ReadByte()
		if err != nil {
			return fmt.Errorf("h2go: skipExtTypeInfo: failed to read GEOMETRY type: %w", err)
		}
		switch typ {
		case 0:
			// No extended info
		case 1:
			// Has type only
			_, _ = tr.ReadInt16()
		case 2:
			// Has SRID only
			_, _ = tr.ReadInt32()
		case 3:
			// Has type and SRID
			_, _ = tr.ReadInt16()
			_, _ = tr.ReadInt32()
		}

	case ValueTypeRow:
		// Read field count
		count, err := tr.ReadInt32()
		if err != nil {
			return fmt.Errorf("h2go: skipExtTypeInfo: failed to read ROW field count: %w", err)
		}
		// Read each field: name (string) + type info
		for i := 0; i < int(count); i++ {
			_, err := tr.ReadString() // field name
			if err != nil {
				return fmt.Errorf("h2go: skipExtTypeInfo: failed to read ROW field name: %w", err)
			}
			_, err = tr.ReadTypeInfo() // field type
			if err != nil {
				return fmt.Errorf("h2go: skipExtTypeInfo: failed to read ROW field type: %w", err)
			}
		}
	}
	return nil
}

// intervalHasFractionalSeconds reports whether the given TI code represents an
// interval type that includes a trailing scale byte (nanosecond precision).
// Only INTERVAL SECOND, DAY TO SECOND, HOUR TO SECOND, and MINUTE TO SECOND
// carry both a precision byte and a scale byte in the TypeInfo frame.
// All other interval types carry only the leading precision byte.
//
// Reference: Transfer.java writeTypeInfo20 / readTypeInfo20, H2 2.4.240.
func intervalHasFractionalSeconds(ti int) bool {
	switch ti {
	case TIIntervalSecond, // 31
		TIIntervalDaySecond, // 35
		TIIntervalHourSec,   // 37
		TIIntervalMinSec:    // 38
		return true
	}
	return false
}

// DatabaseTypeName returns the SQL type name for this TypeInfo.
// Used for driver.RowsColumnTypeDatabaseTypeName implementation.
func (ti *TypeInfo) DatabaseTypeName() string {
	switch ti.ValueType {
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
		return "TIMESTAMP WITH TIME ZONE"
	case ValueTypeTimeTZ:
		return "TIME WITH TIME ZONE"
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
		return "UNKNOWN"
	}
}

// HasPrecisionScale reports whether this type has precision/scale attributes.
func (ti *TypeInfo) HasPrecisionScale() bool {
	switch ti.ValueType {
	case ValueTypeNumeric, ValueTypeDecfloat:
		return true
	default:
		return false
	}
}

// PrecisionScale returns the precision and scale for this type.
// If the type doesn't have precision/scale, returns (0, 0, false).
func (ti *TypeInfo) PrecisionScale() (precision, scale int64, ok bool) {
	if !ti.HasPrecisionScale() {
		return 0, 0, false
	}
	if ti.Precision < 0 {
		return 0, 0, true
	}
	return ti.Precision, int64(ti.Scale), true
}
