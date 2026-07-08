// Package h2go implements a pure Go database/sql driver for H2 Database in
// native TCP server mode. It connects to H2 using native TCP protocol 21
// with H2 version 2.4.240 and later. The driver registers itself with
// database/sql under the name "h2".
//
// # Quick start
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
// The default TCP port is 9092 if omitted.
//
// # Status
//
// This package is under development. Not yet ready for production use.
package h2go
