package h2go

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestStmtImplementsInterfaces(_ *testing.T) {
	var _ driver.Stmt = (*stmt)(nil)
	var _ driver.StmtExecContext = (*stmt)(nil)
	var _ driver.StmtQueryContext = (*stmt)(nil)
	var _ driver.NamedValueChecker = (*stmt)(nil)
}

func TestStmtCheckNamedValue(t *testing.T) {
	s := &stmt{}
	nv := driver.NamedValue{Ordinal: 1, Value: testValuerString("x")}
	if err := s.CheckNamedValue(&nv); err != nil {
		t.Fatalf("CheckNamedValue failed: %v", err)
	}
	if got, ok := nv.Value.(string); !ok || got != "x" {
		t.Fatalf("converted value: got %T(%v), want string(x)", nv.Value, nv.Value)
	}
}

func TestStmtNumInput(t *testing.T) {
	s := &stmt{cmd: &PreparedCommand{ParamCount: 3}}
	if got := s.NumInput(); got != 3 {
		t.Fatalf("NumInput: got %d, want 3", got)
	}
}

func TestStmtCloseIdempotent(t *testing.T) {
	s := &stmt{}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestStmtExecContextClosed(t *testing.T) {
	s := &stmt{closed: true}
	_, err := s.ExecContext(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed error, got %v", err)
	}
}

func TestStmtQueryContextClosed(t *testing.T) {
	s := &stmt{closed: true}
	_, err := s.QueryContext(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed error, got %v", err)
	}
}
