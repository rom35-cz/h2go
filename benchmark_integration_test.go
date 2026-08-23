//go:build integration

// benchmark_integration_test.go — live-server benchmarks: end-to-end round
// trips through database/sql against a running H2 TCP server. Skips without
// JDBC_URL/JDBC_USER/JDBC_PASSWORD. Run with:
//
//	go test -tags=integration -bench Integration -benchmem -run '^$' .
package h2go

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

func benchDB(b *testing.B) *sql.DB {
	b.Helper()
	url := os.Getenv("JDBC_URL")
	user := os.Getenv("JDBC_USER")
	pw := os.Getenv("JDBC_PASSWORD")
	if url == "" || user == "" {
		b.Skip("integration benchmark skipped: env not available")
	}
	cfg, err := ParseDSN(url)
	if err != nil {
		b.Fatalf("ParseDSN: %v", err)
	}
	MergeCredentials(cfg, user, pw)
	db, err := OpenDB(cfg)
	if err != nil {
		b.Fatalf("OpenDB: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

// BenchmarkIntegrationSelectOne measures a full ad-hoc query round trip
// (parse + execute + single-row result drain).
func BenchmarkIntegrationSelectOne(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	var v int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&v); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&v); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIntegrationPreparedSelectOne measures the prepared-statement path:
// statement prepared once, then only execute + result per iteration.
func BenchmarkIntegrationPreparedSelectOne(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	stmt, err := db.PrepareContext(ctx, "SELECT 1")
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()
	var v int
	if err := stmt.QueryRowContext(ctx).Scan(&v); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := stmt.QueryRowContext(ctx).Scan(&v); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIntegrationRows100 measures streaming a 100-row result set end to
// end (execute + metadata + all rows).
func BenchmarkIntegrationRows100(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rows, err := db.QueryContext(ctx, "SELECT X FROM SYSTEM_RANGE(1, 100)")
		if err != nil {
			b.Fatal(err)
		}
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				b.Fatal(err)
			}
		}
		if err := rows.Err(); err != nil {
			b.Fatal(err)
		}
		rows.Close()
	}
}

// BenchmarkIntegrationExecParam measures an update round trip with one bound
// parameter (no generated keys requested).
func BenchmarkIntegrationExecParam(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		"CREATE MEMORY TABLE IF NOT EXISTS BENCH_EXEC(v INT)"); err != nil {
		b.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE BENCH_EXEC") }()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.ExecContext(ContextWithoutGeneratedKeys(ctx),
			"INSERT INTO BENCH_EXEC VALUES (?)", i); err != nil {
			b.Fatal(err)
		}
	}
}
