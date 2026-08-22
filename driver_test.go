package h2go

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
)

func TestDriverRegistration(t *testing.T) {
	// sql.Open with a valid DSN must succeed — any error means the driver
	// is not registered or DSN parsing is broken.
	db, err := sql.Open("h2", "h2://localhost:9092/test")
	if err != nil {
		t.Fatalf("sql.Open failed: %v (driver may not be registered under \"h2\")", err)
	}
	_ = db.Close()
}

func TestDriverOpenInvalidDSN(t *testing.T) {
	d := &Driver{}
	_, err := d.Open("invalid://dsn")
	if err == nil {
		t.Fatal("expected error for invalid DSN scheme")
	}
	// Just verify there's an error message.
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestDriverOpenConnectorInvalidDSN(t *testing.T) {
	d := &Driver{}
	_, err := d.OpenConnector("invalid://dsn")
	if err == nil {
		t.Fatal("expected error for invalid DSN scheme")
	}
}

func TestNewConnectorNilConfig(t *testing.T) {
	_, err := NewConnector(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	// Verify there's an error message.
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestNewConnectorMissingHost(t *testing.T) {
	cfg := &Config{
		Port:     "9092",
		Database: "test",
	}
	_, err := NewConnector(cfg)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestNewConnectorDefaultPort(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "", // empty, should default
		Database: "test",
	}
	connr, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connr == nil {
		t.Fatal("expected non-nil connector")
	}

	// Check the connector has the default port.
	c, ok := connr.(*connector)
	if !ok {
		t.Fatal("expected *connector type")
	}
	if c.cfg.Port != DefaultTCPPortStr {
		t.Errorf("expected default port %s, got %s", DefaultTCPPortStr, c.cfg.Port)
	}
}

func TestNewConnectorValidConfig(t *testing.T) {
	cfg := &Config{
		Host:        "localhost",
		Port:        "9092",
		Database:    "testdb",
		User:        "sa",
		Password:    "password",
		OriginalURL: "h2://localhost:9092/testdb",
	}
	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connector == nil {
		t.Fatal("expected non-nil connector")
	}

	// Verify the Driver() method returns the correct driver.
	d := connector.Driver()
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestConnectorDriverReturnsDriver(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "test",
	}
	c, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := c.Driver()
	if d == nil {
		t.Fatal("expected non-nil driver")
	}

	// The returned driver should be a *Driver.
	_, ok := d.(*Driver)
	if !ok {
		t.Fatalf("expected *Driver, got %T", d)
	}
}

// TestOpenDB verifies OpenDB creates a *sql.DB handle.
// This does not actually connect to a server.
func TestOpenDB(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "test",
	}

	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	if db == nil {
		t.Fatal("expected non-nil *sql.DB")
	}

	// The DB is not actually connected yet; just verify it was created.
	_ = db.Close()
}

func TestOpenDBNilConfig(t *testing.T) {
	_, err := OpenDB(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// TestNewConnectorDoesNotMutateCfg verifies that NewConnector does not write
// back into the caller's Config (Bug C: port defaulting must not mutate).
func TestNewConnectorDoesNotMutateCfg(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "", // intentionally empty
		Database: "test",
	}

	_, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector error: %v", err)
	}

	// The original Config must not be touched.
	if cfg.Port != "" {
		t.Errorf("NewConnector mutated cfg.Port: got %q, want empty string", cfg.Port)
	}
}

// TestNewConnectorCfgIsolated verifies that post-creation mutations to the
// caller's Config do not affect the connector's stored copy (Bug C).
func TestNewConnectorCfgIsolated(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "test",
	}

	connr, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector error: %v", err)
	}

	// Mutate the original config after creation.
	cfg.Host = "otherhost"
	cfg.Port = "9999"

	// The connector must not see the mutations.
	c, ok := connr.(*connector)
	if !ok {
		t.Fatal("expected *connector")
	}
	if c.cfg.Host == "otherhost" {
		t.Error("connector.cfg.Host reflects post-creation mutation (not isolated)")
	}
	if c.cfg.Port == "9999" {
		t.Error("connector.cfg.Port reflects post-creation mutation (not isolated)")
	}
}

// TestNewConnectorCapturesResultOptions verifies that Config.MaxRows and
// Config.FetchSize are captured by the connector without mutating the
// caller's Config.
func TestNewConnectorCapturesResultOptions(t *testing.T) {
	cfg := &Config{
		Host:      "localhost",
		Port:      "9092",
		Database:  "test",
		MaxRows:   25,
		FetchSize: 7,
	}

	connr, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector error: %v", err)
	}
	c, ok := connr.(*connector)
	if !ok {
		t.Fatal("expected *connector")
	}
	if c.cfg.MaxRows != 25 {
		t.Errorf("connector cfg.MaxRows = %d, want 25", c.cfg.MaxRows)
	}
	if c.cfg.FetchSize != 7 {
		t.Errorf("connector cfg.FetchSize = %d, want 7", c.cfg.FetchSize)
	}

	// Mutating the caller's struct after creation must not leak in.
	cfg.MaxRows = 999
	cfg.FetchSize = 999
	if c.cfg.MaxRows != 25 || c.cfg.FetchSize != 7 {
		t.Errorf("connector result options not isolated: MaxRows=%d FetchSize=%d",
			c.cfg.MaxRows, c.cfg.FetchSize)
	}
}

// TestConnEffectiveResultOptions verifies effectiveMaxRows/effectiveFetchSize
// apply defaults and pass through configured values.
func TestConnEffectiveResultOptions(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *Config
		wantMaxRows   int64
		wantFetchSize int
	}{
		{"nil cfg", nil, 0, defaultFetchSize},
		{"zero values", &Config{}, 0, defaultFetchSize},
		{"negative treated as default", &Config{MaxRows: -5, FetchSize: -1}, 0, defaultFetchSize},
		{"configured", &Config{MaxRows: 42, FetchSize: 9}, 42, 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &conn{sess: &Session{cfg: tc.cfg}}
			if got := c.effectiveMaxRows(); got != tc.wantMaxRows {
				t.Errorf("effectiveMaxRows = %d, want %d", got, tc.wantMaxRows)
			}
			if got := c.effectiveFetchSize(); got != tc.wantFetchSize {
				t.Errorf("effectiveFetchSize = %d, want %d", got, tc.wantFetchSize)
			}
		})
	}
}

// TestDriverContextCancellation verifies that Connect respects context
// cancellation when checked before the handshake starts.
func TestDriverContextCancellation(t *testing.T) {
	cfg := &Config{
		Host:     "localhost",
		Port:     "9092",
		Database: "test",
	}

	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector error: %v", err)
	}

	// Cancel the context before connecting.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = connector.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestDriverImplementsInterfaces verifies the driver implements expected
// interfaces at compile time.
func TestDriverImplementsInterfaces(_ *testing.T) {
	// This test just needs to compile; it verifies interface assertions
	// in the code are valid.
	var _ driver.Driver = (*Driver)(nil)
	var _ driver.DriverContext = (*Driver)(nil)
	var _ driver.Connector = (*connector)(nil)
}
