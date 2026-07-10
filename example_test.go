package h2go

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func exampleConfig() (*Config, bool) {
	url := os.Getenv("JDBC_URL")
	user := os.Getenv("JDBC_USER")
	pw := os.Getenv("JDBC_PASSWORD")

	if url == "" || user == "" {
		if vars, ok := loadExampleEnvFile(); ok {
			url = vars["JDBC_URL"]
			user = vars["JDBC_USER"]
			pw = vars["JDBC_PASSWORD"]
		}
	}
	if url == "" || user == "" {
		return nil, false
	}

	cfg, err := ParseDSN(url)
	if err != nil {
		panic(err)
	}
	MergeCredentials(cfg, user, pw)
	return cfg, true
}

func exampleDB() (*sql.DB, bool) {
	cfg, ok := exampleConfig()
	if !ok {
		return nil, false
	}
	db, err := OpenDB(cfg)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, false
	}
	return db, true
}

func loadExampleEnvFile() (map[string]string, bool) {
	for _, path := range []string{
		filepath.Join("h2-data", ".env"),
		filepath.Join("..", "h2-data", ".env"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		vars := make(map[string]string)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				continue
			}
			vars[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
		if vars["JDBC_URL"] != "" && vars["JDBC_USER"] != "" {
			return vars, true
		}
	}
	return nil, false
}

// ExampleOpenDB shows how to open a *sql.DB from a parsed Config.
func ExampleOpenDB() {
	db, ok := exampleDB()
	if !ok {
		return
	}
	defer db.Close()

	// Output:
}

// Example shows query, exec, transaction, and prepared-statement usage.
func Example() {
	db, ok := exampleDB()
	if !ok {
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// QueryContext: lightweight SELECT.
	rows, err := db.QueryContext(ctx, "SELECT 1")
	if err != nil {
		panic(err)
	}
	for rows.Next() {
		var got int64
		if err := rows.Scan(&got); err != nil {
			rows.Close()
			panic(err)
		}
		if got != 1 {
			rows.Close()
			panic(fmt.Errorf("got %d, want 1", got))
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		panic(err)
	}
	if err := rows.Close(); err != nil {
		panic(err)
	}

	// ExecContext: create and populate a table.
	tableExec := fmt.Sprintf("example_exec_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+tableExec+" (id INT PRIMARY KEY, note VARCHAR(32))"); err != nil {
		panic(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+tableExec) }()
	if _, err := db.ExecContext(ctx, "INSERT INTO "+tableExec+" (id, note) VALUES (?, ?)", 1, "hello"); err != nil {
		panic(err)
	}

	// BeginTx: commit a transaction.
	tableTx := fmt.Sprintf("example_tx_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+tableTx+" (id INT PRIMARY KEY, note VARCHAR(32))"); err != nil {
		panic(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+tableTx) }()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		panic(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+tableTx+" (id, note) VALUES (?, ?)", 1, "committed"); err != nil {
		panic(err)
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}

	// PrepareContext: prepare an INSERT and a SELECT.
	tableStmt := fmt.Sprintf("example_stmt_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+tableStmt+" (id INT PRIMARY KEY, note VARCHAR(32))"); err != nil {
		panic(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+tableStmt) }()
	ins, err := db.PrepareContext(ctx, "INSERT INTO "+tableStmt+" (id, note) VALUES (?, ?)")
	if err != nil {
		panic(err)
	}
	if _, err := ins.ExecContext(ctx, 1, "prepared"); err != nil {
		panic(err)
	}
	if err := ins.Close(); err != nil {
		panic(err)
	}
	sel, err := db.PrepareContext(ctx, "SELECT note FROM "+tableStmt+" WHERE id = ?")
	if err != nil {
		panic(err)
	}
	defer sel.Close()
	var note string
	if err := sel.QueryRowContext(ctx, 1).Scan(&note); err != nil {
		panic(err)
	}
	if note != "prepared" {
		panic(fmt.Errorf("got %q, want prepared", note))
	}

	// Output:
}
