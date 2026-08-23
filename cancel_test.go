// cancel_test.go — deep statement cancellation tests (post-v0.2.0 backlog
// item #5): a context cancellation mid-operation fires the side-channel
// SESSION_CANCEL_STATEMENT, the server's aligned "statement was canceled"
// report is received and translated into the context error, and the session
// stays usable afterwards.

package h2go

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFinalizeContextServerCancelKeepsSessionAlive(t *testing.T) {
	s := &Session{tr: mockTransferFromBytes(nil), id: "s1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := error(&Error{Code: ErrorCodeStatementWasCanceled, SQLState: "HY008"})
	s.finalizeContext(ctx, &err)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if s.tr == nil || s.dead.Load() {
		t.Error("session must survive an aligned server-side cancellation report")
	}
}

func TestFinalizeContextOtherErrorWithCanceledCtxStillAborts(t *testing.T) {
	s := &Session{tr: mockTransferFromBytes(nil), id: "s1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := error(&Error{Code: 42001, SQLState: "42000"}) // syntax error, not a cancellation
	s.finalizeContext(ctx, &err)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if s.tr != nil || !s.dead.Load() {
		t.Error("non-cancellation errors during cancellation must abort the session")
	}
}

type sideCancelExpectation struct {
	targetSession string
	op            int32
	statementID   int32
}

// serveOneCancel accepts one side-channel connection and reports what it
// carried over gotCancel.
func serveOneCancel(conn net.Conn, gotCancel chan<- sideCancelExpectation) {
	tr := NewReadWriter(conn)
	_, _ = tr.ReadInt32() // min version
	_, _ = tr.ReadInt32() // max version
	db, _ := tr.ReadString()
	rawURL, _ := tr.ReadString()
	if db != nil || rawURL != nil {
		close(gotCancel)
		return
	}
	sess, _ := tr.ReadString()
	op, _ := tr.ReadInt32()
	stmtID, _ := tr.ReadInt32()
	var target string
	if sess != nil {
		target = *sess
	}
	gotCancel <- sideCancelExpectation{
		targetSession: target,
		op:            op,
		statementID:   stmtID,
	}
	// The reference server writes no reply for cancels; it just stops.
	_ = conn.Close()
}

// TestExecuteQueryCancelledViaSideChannelKeepsSessionUsable drives the full
// flow against a scripted server: the execute response is held until the
// side-channel cancel connection arrives with the expected session/command
// ids, then the aligned canceled-error frame is delivered on the main
// connection. The caller must observe the context deadline error while the
// session remains usable for a follow-up prepare.
func TestExecuteQueryCancelledViaSideChannelKeepsSessionUsable(t *testing.T) {
	mainClient, mainServer := net.Pipe()
	defer mainClient.Close()

	const sessionID = "sess-cancel-test"
	const cmdID = int32(7)

	gotCancel := make(chan sideCancelExpectation, 1)

	go func() {
		tr := NewReadWriter(mainServer)
		// Drain COMMAND_EXECUTE_QUERY request:
		// op . cmdID . resultID . maxRows(8) . fetchSize . paramCount.
		if _, err := tr.ReadInt32(); err != nil {
			return
		}
		if _, err := tr.ReadInt32(); err != nil { // cmd id
			return
		}
		if _, err := tr.ReadInt32(); err != nil { // resultID
			return
		}
		if _, err := tr.ReadInt64(); err != nil { // maxRows
			return
		}
		if _, err := tr.ReadInt32(); err != nil { // fetchSize
			return
		}
		if pc, _ := tr.ReadInt32(); pc != 0 { // paramCount
			return
		}

		// Hold the response until the side-channel cancel arrives.
		info, ok := <-gotCancel
		if !ok || info.targetSession != sessionID ||
			info.op != SessionCancelStatement || info.statementID != cmdID {
			// Wrong or missing cancel: fail loudly by closing the stream.
			_ = mainServer.Close()
			return
		}

		// Aligned server-side cancellation report on the main connection.
		_ = tr.WriteInt32(StatusError)
		_ = tr.WriteString("HY008")
		_ = tr.WriteString("Statement was canceled")
		_ = tr.WriteString("")
		_ = tr.WriteInt32(ErrorCodeStatementWasCanceled)
		_ = tr.WriteString("")
		_ = tr.Flush()

		// Follow-up SESSION_PREPARE on the same connection proves alignment.
		if _, err := tr.ReadInt32(); err != nil { // op
			return
		}
		if _, err := tr.ReadInt32(); err != nil { // command id
			return
		}
		if _, err := tr.ReadString(); err != nil { // sql
			return
		}
		_ = tr.WriteInt32(StatusOK)
		_ = tr.WriteBool(false)
		_ = tr.WriteBool(false)
		_ = tr.WriteInt32(0)
		_ = tr.Flush()
	}()

	// The cancel side channel dials cfg.Host:cfg.Port — serve it locally.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("side-channel listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveOneCancel(conn, gotCancel)
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	s := &Session{
		tr: NewReadWriter(mainClient),
		id: sessionID,
		cfg: &Config{
			Host: "127.0.0.1",
			Port: itoa(port),
		},
	}
	cmd := &PreparedCommand{ID: cmdID, IsQuery: true}

	ctx, cancelCtx := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelCtx()

	rows, err := s.ExecuteQueryPrepared(ctx, cmd, 0, 100)
	if rows != nil {
		_ = rows.Close()
		t.Fatal("expected nil rows from cancelled execute")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if s.tr == nil || s.dead.Load() {
		t.Fatal("session must stay usable after side-channel cancellation")
	}

	// Follow-up operation on the same session/stream must succeed.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	prepared, err := s.PrepareCommand(ctx2, "SELECT 2")
	if err != nil {
		t.Fatalf("follow-up PrepareCommand after cancellation: %v", err)
	}
	if prepared == nil || !strings.Contains(prepared.SQL, "SELECT 2") {
		t.Errorf("unexpected follow-up result %+v", prepared)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
