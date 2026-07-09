package h2go

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// readGeneratedKeysLastInsertID consumes the generated-keys result set that
// follows an update response and, when possible, extracts a single numeric key
// suitable for driver.Result.LastInsertId().
//
// If H2 returned no generated key, multiple keys, or a non-numeric key, the
// returned error wraps ErrLastInsertIDUnavailable. Protocol/I/O failures and H2
// server errors are returned directly.
func (s *Session) readGeneratedKeysLastInsertID() (int64, error) {
	if s == nil || s.tr == nil {
		return 0, fmt.Errorf("h2go: readGeneratedKeysLastInsertID: session closed")
	}

	columnCount, err := s.tr.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("h2go: readGeneratedKeysLastInsertID: failed to read column count: %w", err)
	}
	rowCount, err := s.tr.ReadRowCount()
	if err != nil {
		return 0, fmt.Errorf("h2go: readGeneratedKeysLastInsertID: failed to read row count: %w", err)
	}

	meta, err := s.tr.ReadResultMeta(columnCount, s.version)
	if err != nil {
		return 0, fmt.Errorf("h2go: readGeneratedKeysLastInsertID: failed to read metadata: %w", err)
	}

	if columnCount != 1 || rowCount != 1 {
		if err := s.discardGeneratedKeyRows(meta, rowCount); err != nil {
			return 0, err
		}
		return 0, lastInsertIDUnavailableError("H2 returned %d generated key column(s) across %d row(s)",
			columnCount, rowCount)
	}

	row, err := s.readGeneratedKeyRow(meta)
	if err != nil {
		return 0, err
	}
	if len(row) != 1 {
		return 0, lastInsertIDUnavailableError("H2 returned %d generated key value(s)", len(row))
	}

	id, err := generatedKeyValueToInt64(row[0])
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Session) readGeneratedKeyRow(meta *ResultMeta) ([]driver.Value, error) {
	rowFlag, err := s.tr.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("h2go: readGeneratedKeyRow: failed to read row flag: %w", err)
	}

	switch int8(rowFlag) {
	case 1:
		row := make([]driver.Value, meta.ColumnCount)
		for i := 0; i < int(meta.ColumnCount); i++ {
			col := meta.GetColumn(i)
			if col == nil {
				return nil, fmt.Errorf("h2go: readGeneratedKeyRow: missing metadata for column %d", i)
			}
			value, err := s.tr.ReadValue(col.TypeInfo)
			if err != nil {
				return nil, fmt.Errorf("h2go: readGeneratedKeyRow: failed to read column %d: %w", i, err)
			}
			row[i] = value
		}
		return row, nil
	case 0:
		return nil, lastInsertIDUnavailableError("H2 returned no generated key row")
	case -1:
		return nil, wrapError("readGeneratedKeyRow", readH2Error(s.tr))
	default:
		return nil, fmt.Errorf("h2go: readGeneratedKeyRow: unexpected row flag %d", rowFlag)
	}
}

func (s *Session) discardGeneratedKeyRows(meta *ResultMeta, rowCount int64) error {
	// H2's sendRows(result, count) with count <= 0 exits its while-loop
	// immediately and writes no row bytes at all.
	// rowCount = 0  → INSERT affected 0 rows, or DELETE/UPDATE with no keys.
	// rowCount < 0  → should not happen for LocalResult generated-key results,
	//                 but treat defensively: no bytes to consume.
	if rowCount <= 0 {
		return nil
	}
	readRow := func() error {
		rowFlag, err := s.tr.ReadByte()
		if err != nil {
			return fmt.Errorf("h2go: discardGeneratedKeyRows: failed to read row flag: %w", err)
		}

		switch int8(rowFlag) {
		case 1:
			for i := 0; i < int(meta.ColumnCount); i++ {
				col := meta.GetColumn(i)
				if col == nil {
					return fmt.Errorf("h2go: discardGeneratedKeyRows: missing metadata for column %d", i)
				}
				if _, err := s.tr.ReadValue(col.TypeInfo); err != nil {
					return fmt.Errorf("h2go: discardGeneratedKeyRows: failed to read column %d: %w", i, err)
				}
			}
			return nil
		case 0:
			return ioEOF{}
		case -1:
			return wrapError("discardGeneratedKeyRows", readH2Error(s.tr))
		default:
			return fmt.Errorf("h2go: discardGeneratedKeyRows: unexpected row flag %d", rowFlag)
		}
	}

	if rowCount < 0 {
		// rowCount < 0 is already excluded by the rowCount <= 0 guard above.
		// This branch is unreachable but kept for clarity.
		return nil
	}

	for i := int64(0); i < rowCount; i++ {
		err := readRow()
		if err == nil {
			continue
		}
		if _, ok := err.(ioEOF); ok {
			return nil
		}
		return err
	}
	return nil
}

// ioEOF is a sentinel internal error used to short-circuit result draining when
// the server sends the end-of-result marker.
type ioEOF struct{}

func (ioEOF) Error() string { return "EOF" }

func lastInsertIDUnavailableError(format string, args ...any) error {
	if format == "" {
		return fmt.Errorf("h2go: LastInsertId: %w", ErrLastInsertIDUnavailable)
	}
	return fmt.Errorf("h2go: LastInsertId: "+format+": %w", append(args, ErrLastInsertIDUnavailable)...)
}

func generatedKeyValueToInt64(v driver.Value) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Trunc(x) != x {
			return 0, lastInsertIDUnavailableError("generated key %v is not an integer", x)
		}
		if x < math.MinInt64 || x > math.MaxInt64 {
			return 0, lastInsertIDUnavailableError("generated key %v overflows int64", x)
		}
		return int64(x), nil
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, lastInsertIDUnavailableError("generated key %q is not numeric", x)
		}
		return id, nil
	case []byte:
		id, err := strconv.ParseInt(strings.TrimSpace(string(x)), 10, 64)
		if err != nil {
			return 0, lastInsertIDUnavailableError("generated key %q is not numeric", string(x))
		}
		return id, nil
	case nil:
		return 0, lastInsertIDUnavailableError("H2 returned a NULL generated key")
	default:
		return 0, lastInsertIDUnavailableError("generated key type %T is not numeric", v)
	}
}
