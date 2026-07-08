// result.go — driver.Result implementation for H2 UPDATE/INSERT/DELETE.

package h2go

import (
	"database/sql/driver"
	"errors"
)

// result implements driver.Result for H2 exec operations.
type result struct {
	affected int64
	// lastID is not supported in MVP; see Phase 10 for generated keys support.
}

// LastInsertId returns an error indicating generated keys are not
// supported in the MVP. This method will be implemented in Phase 10
// (T10.1) when full generated keys support is added.
//
// LastInsertId implements driver.Result.
func (r *result) LastInsertId() (int64, error) {
	return 0, errors.New("h2go: LastInsertId not supported in MVP (coming in Phase 10)")
}

// RowsAffected returns the number of rows affected by the
// UPDATE, INSERT, DELETE, or MERGE statement.
//
// RowsAffected implements driver.Result.
func (r *result) RowsAffected() (int64, error) {
	return r.affected, nil
}

// Verify interface compliance at compile time.
var (
	_ driver.Result = (*result)(nil)
)
