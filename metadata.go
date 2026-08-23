// metadata.go — Result set metadata readers.
//
// Reference: org.h2.result.ResultColumn, org.h2.result.ResultRemote

package h2go

import (
	"fmt"
)

// ResultColumn holds metadata for a single result set column.
// This matches the ResultColumn structure from H2's server wire format.
type ResultColumn struct {
	// Alias is the column alias (or computed expression name).
	Alias string

	// SchemaName is the schema name, or empty if not applicable.
	SchemaName string

	// TableName is the table name, or empty if not applicable.
	TableName string

	// ColumnName is the original column name, or empty if computed.
	ColumnName string

	// TypeInfo describes the column's data type.
	TypeInfo *TypeInfo

	// Identity is true if this is an identity (AUTO_INCREMENT) column.
	Identity bool

	// Nullable indicates nullability: 0=unknown, 1=not nullable, 2=nullable.
	Nullable int
}

// ResultMeta holds metadata for all columns in a result set.
type ResultMeta struct {
	// ColumnCount is the number of columns in the result.
	ColumnCount int32

	// Columns contains metadata for each column.
	Columns []ResultColumn
}

// ReadResultMeta reads result column metadata from the transfer stream.
// This is called after COMMAND_EXECUTE_QUERY returns the column count,
// or after COMMAND_GET_META_DATA returns the column count.
//
// The server sends for each column:
//   - alias (string)
//   - schemaName (string)
//   - tableName (string)
//   - columnName (string)
//   - TypeInfo
//   - [protocol < 20: displaySize (int), ignored in protocol 21]
//   - identity (boolean)
//   - nullable (int)
func (tr *Tr) ReadResultMeta(columnCount int32, version int32) (*ResultMeta, error) {
	// Fail-fast guards before pre-allocating Columns from a wire-supplied
	// count: a negative count is a broken frame, and anything above
	// maxWireCollectionElements is treated as a hostile/broken server rather
	// than attempted (compare int64 BEFORE converting to int).
	if columnCount < 0 {
		return nil, fmt.Errorf("h2go: ReadResultMeta: invalid column count %d", columnCount)
	}
	if int64(columnCount) > maxWireCollectionElements {
		return nil, fmt.Errorf("h2go: ReadResultMeta: column count %d exceeds cap %d", columnCount, maxWireCollectionElements)
	}

	meta := &ResultMeta{
		ColumnCount: columnCount,
		Columns:     make([]ResultColumn, columnCount),
	}

	for i := 0; i < int(columnCount); i++ {
		col := ResultColumn{}

		// Read alias
		alias, err := tr.ReadString()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read alias: %w", i, err)
		}
		if alias != nil {
			col.Alias = *alias
		}

		// Read schema name
		schema, err := tr.ReadString()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read schema: %w", i, err)
		}
		if schema != nil {
			col.SchemaName = *schema
		}

		// Read table name
		table, err := tr.ReadString()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read table: %w", i, err)
		}
		if table != nil {
			col.TableName = *table
		}

		// Read column name
		name, err := tr.ReadString()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read column name: %w", i, err)
		}
		if name != nil {
			col.ColumnName = *name
		}

		// Read type info
		typeInfo, err := tr.ReadTypeInfo()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read type info: %w", i, err)
		}
		col.TypeInfo = typeInfo

		// Protocol < 20: skip displaySize field
		if version < TCPProtocolVersion20 {
			_, err := tr.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to skip displaySize: %w", i, err)
			}
		}

		// Read identity flag
		identity, err := tr.ReadBool()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read identity: %w", i, err)
		}
		col.Identity = identity

		// Read nullable status
		nullable, err := tr.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("h2go: ReadResultMeta: column %d: failed to read nullable: %w", i, err)
		}
		col.Nullable = int(nullable)
		// Also set in the embedded TypeInfo for convenience
		col.TypeInfo.Nullable = int(nullable)

		meta.Columns[i] = col
	}

	return meta, nil
}

// ColumnNames returns the column labels as a slice of strings. H2 sends the
// ResultColumn "alias" field, which is the display label JDBC reports via
// getColumnLabel: identical to the column name for plain columns, but the
// expression text or explicit AS-label for expression columns. This is used
// for driver.Rows.Columns() implementation.
func (m *ResultMeta) ColumnNames() []string {
	names := make([]string, len(m.Columns))
	for i, col := range m.Columns {
		names[i] = col.Alias
	}
	return names
}

// GetColumn returns the metadata for a specific column by index (0-based).
// Returns nil if index is out of range.
func (m *ResultMeta) GetColumn(index int) *ResultColumn {
	if index < 0 || index >= len(m.Columns) {
		return nil
	}
	return &m.Columns[index]
}

// GetColumnByName returns the metadata for a specific column by alias name.
// It matches the column label (alias) only: for expression columns such as
// `SELECT 1+1` or `SELECT col AS x`, lookups by the underlying column name
// will not match. Returns nil if no column with that alias exists.
func (m *ResultMeta) GetColumnByName(name string) *ResultColumn {
	for i := range m.Columns {
		if m.Columns[i].Alias == name {
			return &m.Columns[i]
		}
	}
	return nil
}

// NullableString returns a human-readable string for the nullable constant.
func NullableString(n int) string {
	switch n {
	case ColumnNoNulls:
		return "NO_NULLS"
	case ColumnNullable:
		return "NULLABLE"
	case ColumnNullableUnknown:
		return "NULLABLE_UNKNOWN"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", n)
	}
}
