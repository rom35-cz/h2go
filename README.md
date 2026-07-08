# h2go — pure Go H2 Database driver for database/sql

**Status:** Under development. Not yet ready for production use.

`h2go` is a pure Go `database/sql` driver for [H2 Database](https://www.h2database.com/) running in **native TCP server mode**. It connects directly to H2's native TCP protocol — no PostgreSQL compatibility mode, no JDBC bridge, no JVM embedding, no CGO.

## Scope

- H2 version: **2.4.240 and later**
- Native TCP protocol: **21**
- Server mode only: `jdbc:h2:tcp://...`

## Usage

```go
import (
    "database/sql"
    _ "github.com/rom35-cz/h2go"
)

func main() {
    db, err := sql.Open("h2", "jdbc:h2:tcp://localhost:9092/mydb")
    if err != nil {
        panic(err)
    }
    defer db.Close()
}
```

## DSN formats

| Format | Example |
|---|---|
| JDBC-style | `jdbc:h2:tcp://localhost:9092/mydb` |
| Native | `h2://user:pass@localhost:9092/mydb` |
| Native (explicit TCP) | `h2+tcp://user:pass@localhost:9092/mydb` |

Default TCP port: `9092`.

## License

Not yet decided. A `LICENSE` file will be added before the first public release.

## Repository

[github.com/rom35-cz/h2go](https://github.com/rom35-cz/h2go)