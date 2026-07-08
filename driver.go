package h2go

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
)

// Driver implements the database/sql/driver.Driver interface for the
// H2 Database native TCP protocol.
//
// The driver is registered under the name "h2" and can be used with
// sql.Open("h2", dsn) or via NewConnector/OpenDB for programmatic
// configuration with separate credentials.
type Driver struct{}

// Open returns a new connection to the H2 database using the provided
// DSN string. The DSN can be in JDBC-style format or native Go format:
//
//	jdbc:h2:tcp://host:port/database[;PARAM=value]
//	h2://user:password@host:port/database?k=v
//
// Open implements driver.Driver.
func (d *Driver) Open(name string) (driver.Conn, error) {
	cfg, err := ParseDSN(name)
	if err != nil {
		return nil, err
	}
	connector, err := NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return connector.Connect(context.Background())
}

// OpenConnector returns a driver.Connector that can be used to create
// multiple connections with the same configuration.
//
// OpenConnector implements driver.DriverContext.
func (d *Driver) OpenConnector(name string) (driver.Connector, error) {
	cfg, err := ParseDSN(name)
	if err != nil {
		return nil, err
	}
	return NewConnector(cfg)
}

// NewConnector creates a driver.Connector from a parsed Config.
// This allows programmatic configuration where credentials may be
// supplied separately from the URL (e.g. via environment variables),
// which is not possible with sql.Open which only receives one DSN
// string.
//
// NewConnector makes a shallow copy of cfg so that subsequent mutations
// of the caller's struct do not affect the connector, and so that port
// defaulting below does not surprise the caller. Note: the Params map
// is shared (shallow copy); callers should not mutate cfg.Params after
// creating the connector.
//
// Example:
//
//	cfg, err := h2go.ParseDSN("h2://localhost:9092/mydb")
//	if err != nil { ... }
//	h2go.MergeCredentials(cfg, os.Getenv("H2_USER"), os.Getenv("H2_PASS"))
//	connector, err := h2go.NewConnector(cfg)
//	if err != nil { ... }
//	db := sql.OpenDB(connector)
//
// The returned Connector can also be used with sql.Register("h2", &Driver{})
// followed by sql.Open("h2", dsn).
func NewConnector(cfg *Config) (driver.Connector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("h2go: NewConnector: config is nil")
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("h2go: NewConnector: host is required")
	}
	// Shallow-copy so the connector's config is independent of the caller's
	// struct. Port defaulting below does not mutate the caller's value.
	cfgCopy := *cfg
	if cfgCopy.Port == "" {
		cfgCopy.Port = DefaultTCPPortStr
	}
	return &connector{cfg: &cfgCopy}, nil
}

// OpenDB opens a *sql.DB handle using the provided Config.
// This is a convenience function equivalent to:
//
//	connector, err := h2go.NewConnector(cfg)
//	if err != nil { ... }
//	db := sql.OpenDB(connector)
//
// Example with credential merge:
//
//	cfg, _ := h2go.ParseDSN("h2://localhost:9092/mydb")
//	h2go.MergeCredentials(cfg, user, password)
//	db, err := h2go.OpenDB(cfg)
func OpenDB(cfg *Config) (*sql.DB, error) {
	connector, err := NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func init() {
	sql.Register("h2", &Driver{})
}
