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
// And native Go DSNs:
//
//	h2://user:password@host:port/database
//	h2+tcp://user:password@host:port/database
//
// The default TCP port is 9092 if omitted. Diagnostic logging is optional and
// disabled unless a logger is explicitly provided in Config.Logger.
//
// # DSN parameters
//
// Of the DSN parameters, only USER and PASSWORD are consumed (extracted into
// Config). Everything else — JDBC ;KEY=VAL segments and native ?k=v pairs,
// e.g. IFEXISTS, ACCESS_MODE_DATA, AUTO_SERVER — is parsed into Config.Params
// but not applied: the driver neither forwards it to the server nor enforces
// it locally. Validate such settings manually before connecting. With
// Config.Logger at debug level, each connection logs the ignored keys (never
// their values).
//
// # Result options
//
// Config.MaxRows bounds the server-side size of each result set (forwarded as
// the protocol maxRows; 0 means unlimited). Config.FetchSize controls how many
// rows are prefetched per batch while streaming results (0 means the driver
// default of 100).
//
// # Status
//
// This package is under development. Not yet ready for production use.
package h2go
