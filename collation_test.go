//go:build integration

// collation_test.go — investigation/verification of language-aware sorting on
// the database side (the H2 analog of Oracle's NLS_SORT/NLS_COMP):
//
//	SET COLLATION <locale> [STRENGTH PRIMARY|SECONDARY|TERTIARY|IDENTICAL]
//
// Per H2 2.4.240 source (Set.java case SetTypes.COLLATION): the collation is
// a DATABASE property, admin-only, and changeable only while the database has
// no user tables. The driver's role is plain SQL passthrough plus correct
// UTF-8 handling of Czech text in parameters and results — both exercised
// here with bound-parameter inserts and ordered scans.
//
// Uses two dedicated throwaway databases so the shared test database's
// collation is never touched.

package h2go

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestIntegration_CzechCollationSorting(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	ctx := context.Background()

	// Czech alphabet relevant here: ... c č d ... h CH i ... r ř s š ... z ž.
	// The "ch" digraph sorts after all h-words; č sorts between c and d;
	// ž sorts last.
	czechExpected := []string{
		"auto", "cena", "čaj", "dům",
		"hlava", "hora", "chata", "chrám",
		"mrkev", "řeka", "švestka", "třída",
		"zvíře", "žába",
	}
	// Default (OFF) collation compares by code point: unaccented letters
	// first (a c ch d h m t z), then accented code points in Unicode order
	// (č 0x10D, ř 0x159, š 0x161, ž 0x17E).
	binaryExpected := []string{
		"auto", "cena", "chata", "chrám", "dům",
		"hlava", "hora", "mrkev", "třída",
		"zvíře", "čaj", "řeka", "švestka",
		"žába",
	}

	words := []string{"auto", "čaj", "cena", "dům", "hlava", "hora",
		"chata", "chrám", "mrkev", "řeka", "švestka", "třída", "zvíře", "žába"}

	openFresh := func(dbName string) *sql.DB {
		t.Helper()
		cfg, err := ParseDSN(env["JDBC_URL"])
		if err != nil {
			t.Fatalf("ParseDSN: %v", err)
		}
		MergeCredentials(cfg, env["JDBC_USER"], env["JDBC_PASSWORD"])
		cfg.Database = dbName + "_" + uuid.NewString()[:8]
		db, err := OpenDB(cfg)
		if err != nil {
			t.Fatalf("OpenDB(%s): %v", cfg.Database, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		// Make re-runs possible: SET COLLATION only works on a tableless DB.
		_, _ = db.ExecContext(ctx, "DROP SCHEMA PUBLIC CASCADE")
		return db
	}

	seedAndReadOrder := func(t *testing.T, db *sql.DB, collation string) []string {
		t.Helper()
		if collation != "" {
			if _, err := db.ExecContext(ctx, "SET COLLATION "+collation); err != nil {
				t.Fatalf("SET COLLATION %s: %v", collation, err)
			}
			// H2 folds the unquoted locale identifier, so the stored setting
			// reads e.g. "CS_CZ STRENGTH TERTIARY".
			var mode string
			if err := db.QueryRowContext(ctx,
				"SELECT SETTING_VALUE FROM INFORMATION_SCHEMA.SETTINGS WHERE SETTING_NAME = 'COLLATION'").
				Scan(&mode); err != nil || !strings.Contains(strings.ToLower(mode), "cs_cz") {
				t.Errorf("COLLATION setting = %q (err %v), want it to contain cs_CZ", mode, err)
			}
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE slova(id INT PRIMARY KEY, w VARCHAR(100))"); err != nil {
			t.Fatalf("CREATE TABLE: %v", err)
		}
		for i, w := range words {
			// Bound parameters exercise the driver's UTF-8 encoding of Czech
			// diacritics end to end.
			if _, err := db.ExecContext(ctx,
				"INSERT INTO slova (id, w) VALUES (?, ?)", i+1, w); err != nil {
				t.Fatalf("INSERT %q: %v", w, err)
			}
		}
		rows, err := db.QueryContext(ctx, "SELECT w FROM slova ORDER BY w")
		if err != nil {
			t.Fatalf("ORDER BY query: %v", err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var w string
			if err := rows.Scan(&w); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got = append(got, w)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return got
	}

	equalWords := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("czech collation cs_CZ", func(t *testing.T) {
		db := openFresh("collation_cs_go")
		got := seedAndReadOrder(t, db, "cs_CZ")
		if !equalWords(got, czechExpected) {
			t.Errorf("Czech sort order wrong.\n got: %q\nwant: %q", got, czechExpected)
		}
	})

	t.Run("default collation is not czech", func(t *testing.T) {
		db := openFresh("collation_bin_go")
		got := seedAndReadOrder(t, db, "")
		if equalWords(got, czechExpected) {
			t.Errorf("default collation unexpectedly matches Czech order: %q", got)
		}
		if !equalWords(got, binaryExpected) {
			t.Errorf("default (binary) order unexpected.\n got: %q\nwant: %q", got, binaryExpected)
		}
	})
}
