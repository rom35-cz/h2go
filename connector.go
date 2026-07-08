package h2go

import (
	"context"
	"database/sql/driver"
)

// connector implements driver.Connector, providing a way to create
// multiple connections with the same configuration. It is returned
// by NewConnector and can be used with sql.OpenDB.
type connector struct {
	cfg *Config
}

// Connect establishes a new connection to the H2 database.
// It performs the full TCP handshake authentication sequence.
//
// Connect implements driver.Connector.
func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	// TODO(T9.2): Apply context deadline to the dial and handshake.
	// For now, we perform blocking I/O and check for cancellation
	// before and after the expensive operations.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	sess, err := Handshake(c.cfg)
	if err != nil {
		return nil, err
	}

	// Check for cancellation after handshake completes.
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return nil, ctx.Err()
	default:
	}

	return &conn{
		sess: sess,
	}, nil
}

// Driver returns the underlying Driver instance.
//
// Driver implements driver.Connector.
func (c *connector) Driver() driver.Driver {
	return &Driver{}
}

// Verify interface compliance at compile time.
var (
	_ driver.Driver        = (*Driver)(nil)
	_ driver.DriverContext = (*Driver)(nil)
	_ driver.Connector     = (*connector)(nil)
)
