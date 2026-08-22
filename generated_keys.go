package h2go

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// GeneratedKeysResult holds the full generated keys result set returned by H2
// after an INSERT or MERGE statement. It provides access to both single and
// multi-column generated keys through the Rows field.
type GeneratedKeysResult struct {
	// Columns holds the column names of the generated keys result.
	Columns []string
	// Rows holds the generated key values, one entry per row.
	Rows [][]driver.Value
}

// SingleInt64 returns the single numeric generated key when the result contains
// exactly one row with one numeric column. It is equivalent to the MVP
// LastInsertId() path.
func (gkr *GeneratedKeysResult) SingleInt64() (int64, error) {
	if gkr == nil {
		return 0, lastInsertIDUnavailableError("no generated keys result")
	}
	if len(gkr.Rows) != 1 || len(gkr.Columns) != 1 {
		return 0, lastInsertIDUnavailableError("H2 returned %d generated key column(s) across %d row(s)",
			len(gkr.Columns), len(gkr.Rows))
	}
	if len(gkr.Rows[0]) != 1 {
		return 0, lastInsertIDUnavailableError("H2 returned %d generated key value(s)", len(gkr.Rows[0]))
	}
	return generatedKeyValueToInt64(gkr.Rows[0][0])
}

// readGeneratedKeys reads the full generated-keys result set that follows an
// update response. It returns the parsed result and also extracts the single
// numeric LastInsertID when possible.
func (s *Session) readGeneratedKeys() (*GeneratedKeysResult, int64, error) {
	if s == nil || s.tr == nil {
		return nil, 0, fmt.Errorf("h2go: readGeneratedKeys: session closed")
	}

	columnCount, err := s.tr.ReadInt32()
	if err != nil {
		return nil, 0, fmt.Errorf("h2go: readGeneratedKeys: failed to read column count: %w", err)
	}
	rowCount, err := s.tr.ReadRowCount()
	if err != nil {
		return nil, 0, fmt.Errorf("h2go: readGeneratedKeys: failed to read row count: %w", err)
	}

	meta, err := s.tr.ReadResultMeta(columnCount, s.version)
	if err != nil {
		return nil, 0, fmt.Errorf("h2go: readGeneratedKeys: failed to read metadata: %w", err)
	}

	// Build the result.
	result := &GeneratedKeysResult{
		Columns: meta.ColumnNames(),
	}

	// Read all rows. For rowCount <= 0 there is nothing to consume: H2's
	// sendRows exits immediately when count <= 0, so no end-of-row marker
	// is sent either.
	if rowCount > 0 {
		result.Rows = make([][]driver.Value, 0, rowCount)
		for i := int64(0); i < rowCount; i++ {
			row, err := s.readGeneratedKeyRow(meta)
			if err != nil {
				// ioEOF means the server signalled end-of-result early
				// (fewer rows than advertised). Stop reading.
				if _, ok := err.(ioEOF); ok {
					break
				}
				return nil, 0, err
			}
			result.Rows = append(result.Rows, row)
		}
	} else if rowCount < 0 {
		// rowCount < 0: eager result, read until ioEOF.
		for {
			row, err := s.readGeneratedKeyRow(meta)
			if err != nil {
				if _, ok := err.(ioEOF); ok {
					break
				}
				return nil, 0, err
			}
			result.Rows = append(result.Rows, row)
		}
	}

	// Try to extract the single numeric LastInsertID.
	lastInsertID, idErr := result.SingleInt64()
	return result, lastInsertID, idErr
}

// readGeneratedKeysLastInsertID consumes the generated-keys result set that
// follows an update response and, when possible, extracts a single numeric key
// suitable for driver.Result.LastInsertId().
//
// Deprecated: Use readGeneratedKeys instead, which returns both the full
// result and the single numeric key.
func (s *Session) readGeneratedKeysLastInsertID() (int64, error) {
	_, id, err := s.readGeneratedKeys()
	if err != nil && !isLastInsertIDUnavailable(err) {
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
		return nil, ioEOF{}
	case -1:
		return nil, wrapError("readGeneratedKeyRow", readH2Error(s.tr))
	default:
		return nil, fmt.Errorf("h2go: readGeneratedKeyRow: unexpected row flag %d", rowFlag)
	}
}

func (s *Session) discardGeneratedKeyRows(meta *ResultMeta, rowCount int64) error {
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

func isLastInsertIDUnavailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrLastInsertIDUnavailable.Error())
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
