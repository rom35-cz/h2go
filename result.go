// result.go — driver.Result implementation for H2 UPDATE/INSERT/DELETE.

package h2go

import (
	"database/sql/driver"
	"fmt"
)

// result implements driver.Result for H2 exec operations.
type result struct {
	affected int64

	lastInsertID    int64
	lastInsertIDSet bool
	lastInsertErr   error

	// GeneratedKeys holds the full generated-keys result when available.
	GeneratedKeys *GeneratedKeysResult
}

// LastInsertId returns the generated key when H2 returned exactly one numeric
// generated key for the statement.
//
// When H2 returned no key, multiple keys, or a non-numeric key, the returned
// error wraps ErrLastInsertIDUnavailable.
//
// LastInsertId implements driver.Result.
func (r *result) LastInsertId() (int64, error) {
	if r == nil {
		return 0, fmt.Errorf("h2go: LastInsertId: %w", ErrLastInsertIDUnavailable)
	}
	if !r.lastInsertIDSet {
		if r.lastInsertErr != nil {
			return 0, r.lastInsertErr
		}
		return 0, fmt.Errorf("h2go: LastInsertId: %w", ErrLastInsertIDUnavailable)
	}
	return r.lastInsertID, nil
}

// RowsAffected returns the number of rows affected by the
// UPDATE, INSERT, DELETE, or MERGE statement.
//
// RowsAffected implements driver.Result.
func (r *result) RowsAffected() (int64, error) {
	if r == nil {
		return 0, nil
	}
	return r.affected, nil
}

// Verify interface compliance at compile time.
var (
	_ driver.Result = (*result)(nil)
)
