// Package h2go implements a pure Go database/sql driver for H2 Database in
// native TCP server mode. It connects to H2 using native TCP protocol 21
// with H2 version 2.4.240 and later. The driver registers itself with
// database/sql under the name "h2".
//
// # Quick start
//
// Import the driver for registration and open a JDBC-style H2 TCP DSN:
//
//	import (
//	    "database/sql"
//	    _ "github.com/rom35-cz/h2go"
//	)
//
//	func main() {
//	    db, err := sql.Open("h2", "jdbc:h2:tcp://localhost:9092/h2-go")
//	    if err != nil {
//	        panic(err)
//	    }
//	    defer db.Close()
//	}
//
// For explicit configuration, parse the DSN and supply credentials and a
// logger separately:
//
//	cfg, err := h2go.ParseDSN(os.Getenv("JDBC_URL"))
//	if err != nil {
//	    panic(err)
//	}
//	h2go.MergeCredentials(cfg, os.Getenv("JDBC_USER"), os.Getenv("JDBC_PASSWORD"))
//	cfg.Logger = h2go.NewTextLogger(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
//	db, err := h2go.OpenDB(cfg)
//	if err != nil {
//	    panic(err)
//	}
//
// # DSN formats
//
// The driver supports JDBC-style H2 TCP URLs:
//
//	jdbc:h2:tcp://host:port/database
//
// native Go DSNs:
//
//	h2://user:password@host:port/database
//	h2+tcp://user:password@host:port/database
//
// and their TLS variants (for servers started with -tcpSSL):
//
//	jdbc:h2:ssl://host:port/database
//	h2+ssl://user:password@host:port/database
//
// The default TCP port is 9092 if omitted. Diagnostic logging is optional and
// disabled unless a logger is explicitly provided in Config.Logger.
//
// # DSN parameters
//
// DSN settings follow H2's own client policy: USER and PASSWORD are consumed;
// server-enforced settings (IFEXISTS, ACCESS_MODE_DATA, INIT, MODE,
// LOCK_TIMEOUT, FORBID_CREATION) are forwarded in the handshake property map;
// embedded/JDBC-client-only settings are accepted but have no effect; and any
// unknown setting is rejected unless the DSN carries
// IGNORE_UNKNOWN_SETTINGS=TRUE.
//
// # Result options
//
// Config.MaxRows bounds the server-side size of each result set (forwarded as
// the protocol maxRows; 0 means unlimited). Config.FetchSize controls how many
// rows are prefetched per batch while streaming results (0 means the driver
// default of 100).
//
// # Generated keys and per-statement overrides
//
// Generated keys are requested automatically for updates, like JDBC's
// Statement.RETURN_GENERATED_KEYS; the connection-level mode family lives in
// Config (GeneratedKeysMode & friends), and ContextWithGeneratedKeys /
// ContextWithoutGeneratedKeys override it for a single statement. The full
// multi-column/multi-row keys result is reachable through
// GeneratedKeysProvider on results obtained via sql.Conn.Raw.
//
// # Status
//
// This package is under development. Not yet ready for production use.
// Licensed under the MIT license; see LICENSE.
package h2go
