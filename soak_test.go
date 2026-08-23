//go:build integration

// soak_test.go — Tier B bounded soak: sustained pool churn with leak metrics.
//
// Skipped unless H2GO_SOAK_SECONDS is set (e.g. `make soak` runs 60s; set a
// higher value for overnight runs). Tracks goroutines, heap and open file
// descriptors across thousands of mixed operations — including deep-canceled
// queries — and fails on unbounded growth or any operation error.

package h2go

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

type soakMetrics struct {
	Goroutines int
	HeapAlloc  uint64
	FDs        int
}

func sampleSoakMetrics() soakMetrics {
	m := soakMetrics{Goroutines: runtime.NumGoroutine()}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.HeapAlloc = ms.HeapAlloc
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		m.FDs = len(entries)
	}
	return m
}

func TestIntegration_Soak(t *testing.T) {
	env := integrationEnv(t)
	if env == nil {
		t.Skip("integration test skipped: env not available")
	}
	seconds := 0
	if v := os.Getenv("H2GO_SOAK_SECONDS"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &seconds); err != nil || seconds <= 0 {
			t.Fatalf("invalid H2GO_SOAK_SECONDS %q", v)
		}
	}
	if seconds == 0 {
		t.Skip("soak skipped: set H2GO_SOAK_SECONDS to enable (make soak)")
	}

	db := integrationDB(t, env)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS soak_scratch(v INT PRIMARY KEY, pad VARCHAR(40))"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE soak_scratch") }()

	prepared, err := db.PrepareContext(ctx,
		"SELECT v FROM soak_scratch WHERE v = ?")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer prepared.Close()

	baseline := sampleSoakMetrics()
	samples := []soakMetrics{baseline}
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				samples = append(samples, sampleSoakMetrics())
			case <-stop:
				return
			}
		}
	}()

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	i := 0
	var firstErr error
	for time.Now().Before(deadline) && firstErr == nil {
		i++
		switch i % 5 {
		case 0: // streaming result
			rows, qerr := db.QueryContext(ctx,
				"SELECT X FROM SYSTEM_RANGE(1, 50)")
			if qerr != nil {
				firstErr = fmt.Errorf("iter %d stream query: %w", i, qerr)
				break
			}
			n := 0
			for rows.Next() {
				var v int64
				if serr := rows.Scan(&v); serr != nil {
					firstErr = fmt.Errorf("iter %d stream scan: %w", i, serr)
					break
				}
				n++
			}
			if ferr := rows.Err(); ferr != nil {
				firstErr = fmt.Errorf("iter %d stream rows: %w", i, ferr)
			}
			rows.Close()
			if n != 50 && firstErr == nil {
				firstErr = fmt.Errorf("iter %d streamed %d rows, want 50", i, n)
			}

		case 1: // transaction rollback
			tx, terr := db.BeginTx(ctx, nil)
			if terr != nil {
				firstErr = fmt.Errorf("iter %d begin: %w", i, terr)
				break
			}
			if _, eerr := tx.ExecContext(ctx,
				"INSERT INTO soak_scratch VALUES (?, 'rollback')", -i); eerr != nil {
				firstErr = fmt.Errorf("iter %d tx insert: %w", i, eerr)
				break
			}
			if rerr := tx.Rollback(); rerr != nil {
				firstErr = fmt.Errorf("iter %d rollback: %w", i, rerr)
			}

		case 2: // transaction commit
			tx, terr := db.BeginTx(ctx, nil)
			if terr != nil {
				firstErr = fmt.Errorf("iter %d begin: %w", i, terr)
				break
			}
			if _, eerr := tx.ExecContext(ctx,
				"MERGE INTO soak_scratch KEY(v) VALUES (?, 'commit')", i); eerr != nil {
				tx.Rollback()
				firstErr = fmt.Errorf("iter %d tx merge: %w", i, eerr)
				break
			}
			if cerr := tx.Commit(); cerr != nil {
				firstErr = fmt.Errorf("iter %d commit: %w", i, cerr)
			}

		case 3: // deep cancellation of a heavy query
			cctx, cancel := context.WithTimeout(ctx, 120*time.Millisecond)
			_, qerr := db.QueryContext(cctx, faultLongQuery)
			cancel()
			if qerr != nil && !errors.Is(qerr, context.DeadlineExceeded) {
				firstErr = fmt.Errorf("iter %d cancel-query: %w", i, qerr)
			}

		default: // point query via prepared statement
			var v int64
			if qerr := prepared.QueryRowContext(ctx, i%1000).Scan(&v); qerr != nil &&
				!errors.Is(qerr, sql.ErrNoRows) {
				firstErr = fmt.Errorf("iter %d prepared: %w", i, qerr)
			}
		}
	}
	close(stop)

	if firstErr != nil {
		t.Fatalf("soak failed after %d operations: %v", i, firstErr)
	}

	final := sampleSoakMetrics()
	t.Logf("soak: %d ops in %ds | goroutines %d→%d | heap %.1f→%.1f MiB | fds %d→%d",
		i, seconds,
		baseline.Goroutines, final.Goroutines,
		float64(baseline.HeapAlloc)/(1<<20), float64(final.HeapAlloc)/(1<<20),
		baseline.FDs, final.FDs)

	// Leak guards with generous tolerances: transient pools/GC noise allowed,
	// unbounded growth is not.
	if maxG := baseline.Goroutines + 25; final.Goroutines > maxG {
		t.Errorf("goroutine leak: %d → %d (max %d)", baseline.Goroutines, final.Goroutines, maxG)
	}
	if baseline.FDs > 0 && final.FDs > baseline.FDs+16 {
		t.Errorf("fd leak: %d → %d", baseline.FDs, final.FDs)
	}

	// Peak goroutines must also stay bounded (no mid-run explosion).
	maxSeen := 0
	for _, s := range samples {
		if s.Goroutines > maxSeen {
			maxSeen = s.Goroutines
		}
	}
	if maxG := baseline.Goroutines + 60; maxSeen > maxG {
		t.Errorf("peak goroutines %d exceeded %d", maxSeen, maxG)
	}
}
