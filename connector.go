package h2go

import (
	"context"
	"database/sql/driver"
	"log/slog"
	"sort"
	"strings"
)

// defaultDriver is the shared Driver value returned by Connector.Driver.
// Driver is stateless, so there is no reason to allocate a new one per call.
var defaultDriver = &Driver{}

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
	logIgnoredParams(c.cfg)
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

// logIgnoredParams emits one debug record listing DSN parameters that were
// parsed into Config.Params but are not applied by this driver (finding 7:
// IFEXISTS, AUTO_SERVER, ACCESS_MODE_DATA, file-lock settings, etc. silently
// no-op). Only keys are listed — values may contain credentials and are never
// logged, so no redaction machinery is involved.
// It returns true when a record was emitted, false when diagnostics are
// disabled or there is nothing to report.
func logIgnoredParams(cfg *Config) bool {
	if cfg == nil || cfg.Logger == nil || len(cfg.Params) == 0 {
		return false
	}
	keys := make([]string, 0, len(cfg.Params))
	for k := range cfg.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	logConfig(cfg, slog.LevelDebug,
		"DSN parameters parsed but not applied by this driver",
		slog.Int("count", len(keys)),
		slog.String("keys", strings.Join(keys, ",")))
	return true
}

// Driver returns the underlying Driver instance.
//
// Driver implements driver.Connector.
func (c *connector) Driver() driver.Driver {
	return defaultDriver
}

// Verify interface compliance at compile time.
var (
	_ driver.Driver        = (*Driver)(nil)
	_ driver.DriverContext = (*Driver)(nil)
	_ driver.Connector     = (*connector)(nil)
)
