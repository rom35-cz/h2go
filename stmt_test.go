package h2go

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
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

func TestStmtCloseSendsCommandClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		tr := NewReadWriter(serverConn)
		op, err := tr.ReadInt32()
		if err != nil {
			errCh <- err
			return
		}
		id, err := tr.ReadInt32()
		if err != nil {
			errCh <- err
			return
		}
		if op != CommandClose || id != 77 {
			errCh <- fmt.Errorf("got op=%d id=%d, want op=%d id=77", op, id, CommandClose)
			return
		}
		errCh <- nil
	}()

	c := &conn{sess: &Session{tr: NewReadWriter(clientConn), id: "test"}}
	s := &stmt{conn: c, cmd: &PreparedCommand{ID: 77}}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server read failed: %v", err)
	}
	if got := s.NumInput(); got != -1 {
		t.Fatalf("NumInput after close = %d, want -1", got)
	}
}

func TestStmtCloseWhileBusyDefersCommandClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		tr := NewReadWriter(serverConn)
		op, err := tr.ReadInt32()
		if err != nil {
			errCh <- err
			return
		}
		id, err := tr.ReadInt32()
		if err != nil {
			errCh <- err
			return
		}
		if op != CommandClose || id != 88 {
			errCh <- fmt.Errorf("got op=%d id=%d, want op=%d id=88", op, id, CommandClose)
			return
		}
		errCh <- nil
	}()

	c := &conn{sess: &Session{tr: NewReadWriter(clientConn), id: "test"}}
	s := &stmt{conn: c, cmd: &PreparedCommand{ID: 88}}

	ownedConn, _, err := s.beginOperation()
	if err != nil {
		t.Fatalf("beginOperation failed: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close while busy failed: %v", err)
	}
	s.mu.Lock()
	if !s.closed || !s.closePending || s.cmd == nil {
		t.Fatalf("Close while busy should keep command pending; closed=%v pending=%v cmd=%v", s.closed, s.closePending, s.cmd)
	}
	s.mu.Unlock()

	if err := s.finishOperation(ownedConn); err != nil {
		t.Fatalf("finishOperation failed: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server read failed: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closeSent || s.closePending || s.cmd != nil || s.conn != nil {
		t.Fatalf("deferred close state invalid: sent=%v pending=%v cmd=%v conn=%v",
			s.closeSent, s.closePending, s.cmd, s.conn)
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
