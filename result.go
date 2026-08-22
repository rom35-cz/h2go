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

// GetGeneratedKeys returns the full generated-keys result carried by this
// result, or nil when no keys were requested or none were returned.
//
// GetGeneratedKeys makes *result satisfy GeneratedKeysProvider.
func (r *result) GetGeneratedKeys() *GeneratedKeysResult {
	if r == nil {
		return nil
	}
	return r.GeneratedKeys
}

// GeneratedKeysProvider is implemented by the driver.Result values h2go
// returns when generated keys were requested. It exposes the full
// multi-column / multi-row key result beyond the single-value LastInsertId.
//
// # Reachability
//
// database/sql wraps every driver result in its own unexported type before
// returning it as a sql.Result, so a result from db.Exec or
// DB.ExecContext can NOT be asserted to this interface. Obtain the
// driver-level result through sql.Conn.Raw instead:
//
//	sqlConn, _ := db.Conn(ctx)
//	defer sqlConn.Close()
//	_ = sqlConn.Raw(func(raw any) error {
//		c := raw.(driver.Conn) // *h2go.conn, implements driver.ExecerContext
//		res, err := c.(driver.ExecerContext).ExecContext(ctx,
//			"INSERT INTO t(x) VALUES (1)", nil)
//		if err != nil {
//			return err
//		}
//		if gkp, ok := res.(h2go.GeneratedKeysProvider); ok {
//			keys = gkp.GetGeneratedKeys()
//		}
//		return nil
//	})
type GeneratedKeysProvider interface {
	GetGeneratedKeys() *GeneratedKeysResult
}

// Verify interface compliance at compile time.
var (
	_ driver.Result         = (*result)(nil)
	_ GeneratedKeysProvider = (*result)(nil)
)
