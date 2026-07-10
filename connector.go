package h2go

import (
	"context"
	"database/sql/driver"
	"log/slog"
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
	if c == nil || c.cfg == nil {
		return nil, driver.ErrBadConn
	}

	select {
	case <-ctx.Done():
		logConfig(c.cfg, slog.LevelDebug, "connect aborted before handshake", slog.String("reason", ctx.Err().Error()))
		return nil, ctx.Err()
	default:
	}

	logConfig(c.cfg, slog.LevelDebug, "connect starting")
	sess, err := HandshakeContext(ctx, c.cfg)
	if err != nil {
		return nil, err
	}

	// Check for cancellation after handshake completes.
	select {
	case <-ctx.Done():
		_ = sess.Close()
		logConfig(c.cfg, slog.LevelDebug, "connect cancelled after handshake", slog.String("reason", ctx.Err().Error()))
		return nil, ctx.Err()
	default:
	}

	logConfig(c.cfg, slog.LevelDebug, "connect established")
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
