// generated_keys_ctx.go — per-statement generated-keys overrides.
//
// The Config.GeneratedKeysMode family is connection-level: every update on
// connections from that Config uses the same mode. This file adds the
// statement-level escape hatch: a generated-keys request attached to the
// ExecContext's context wins for that single statement only.
//
// Context values are the idiomatic database/sql channel for per-call driver
// options — database/sql forwards the caller's ctx to QueryerContext /
// ExecerContext / StmtExecContext, and prepared statements reach the same
// resolution point through StmtExecContext.
//
// Precedence: per-statement override > Config > default (auto). An unknown
// Mode in an override falls back to the connection configuration rather than
// guessing.

package h2go

import (
	"context"
)

type genKeysCtxKey struct{}

// GeneratedKeysRequest describes a per-statement generated-keys request,
// attached to a context with ContextWithGeneratedKeys.
//
// The zero value requests auto mode (like JDBC
// Statement.RETURN_GENERATED_KEYS); use GeneratedKeysRequest.Mode with one of
// the GeneratedKeys* constants to be explicit.
type GeneratedKeysRequest struct {
	// Mode selects the request style: GeneratedKeysAuto (default),
	// GeneratedKeysNone, GeneratedKeysColumnNumbers or
	// GeneratedKeysColumnNames.
	Mode int

	// Columns holds 1-based column indices for Mode ==
	// GeneratedKeysColumnNumbers.
	Columns []int

	// Names holds column names for Mode == GeneratedKeysColumnNames.
	Names []string
}

// ContextWithGeneratedKeys returns a child context carrying req. Passing the
// result to ExecContext/QueryContext makes that single statement use req
// instead of the connection-level configuration:
//
//	ctx := h2go.ContextWithGeneratedKeys(ctx, h2go.GeneratedKeysRequest{
//	    Mode:    h2go.GeneratedKeysColumnNames,
//	    Names:   []string{"ID"},
//	})
//	res, err := db.ExecContext(ctx, "INSERT INTO t(name) VALUES (?)", name)
func ContextWithGeneratedKeys(ctx context.Context, req GeneratedKeysRequest) context.Context {
	return context.WithValue(ctx, genKeysCtxKey{}, req)
}

// ContextWithoutGeneratedKeys returns a child context that suppresses
// generated-key requests for the single statement it reaches — the
// per-statement equivalent of Config.GeneratedKeysNone with
// GeneratedKeysModeSet, useful because GeneratedKeysNone == 0 cannot be
// expressed by omission.
func ContextWithoutGeneratedKeys(ctx context.Context) context.Context {
	return ContextWithGeneratedKeys(ctx, GeneratedKeysRequest{Mode: GeneratedKeysNone})
}

// generatedKeysOverride extracts a per-statement request from ctx, if any.
func generatedKeysOverride(ctx context.Context) (GeneratedKeysRequest, bool) {
	if ctx == nil {
		return GeneratedKeysRequest{}, false
	}
	req, ok := ctx.Value(genKeysCtxKey{}).(GeneratedKeysRequest)
	return req, ok
}

// resolveGeneratedKeys merges the per-statement override (if any) with the
// session configuration into the concrete wire parameters: mode plus selector
// columns/names. Unknown override modes fall back to the connection config.
func (s *Session) resolveGeneratedKeys(ctx context.Context) (mode int32, cols []int, names []string) {
	if req, ok := generatedKeysOverride(ctx); ok {
		switch req.Mode {
		case GeneratedKeysNone:
			return GeneratedKeysNone, nil, nil
		case GeneratedKeysAuto:
			return GeneratedKeysAuto, nil, nil
		case GeneratedKeysColumnNumbers:
			return GeneratedKeysColumnNumbers, req.Columns, nil
		case GeneratedKeysColumnNames:
			return GeneratedKeysColumnNames, nil, req.Names
		default:
			// Unknown override: fall back to the connection configuration.
		}
	}

	mode = s.generatedKeysMode()
	switch mode {
	case GeneratedKeysColumnNumbers:
		cols = s.cfg.GeneratedKeysColumns
	case GeneratedKeysColumnNames:
		names = s.cfg.GeneratedKeysColumnNames
	}
	return mode, cols, names
}
