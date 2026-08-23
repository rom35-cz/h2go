// stream_discard_test.go — unit tests for deterministic session discard on
// mid-stream decode errors (docs/internal/MATURITY_ROUND_II_PLAN.md Task 5, finding 6).

package h2go

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"time"
)

// newDiscardTestRows builds a minimal single-column Rows over the given
// session for discard-rule tests.
func newDiscardTestRows(sess *Session) *Rows {
	return &Rows{
		session:     sess,
		columnCount: 1,
		columns: &ResultMeta{
			ColumnCount: 1,
			Columns:     []ResultColumn{{Alias: "c"}},
		},
		fetchSize: 10,
		rowCount:  -1,
	}
}

// writeH2ErrorFrameTr writes a complete server-side exception frame
// (sqlState . message . sql . errorCode . stackTrace), which readH2Error
// consumes fully, leaving the stream aligned.
func writeH2ErrorFrameTr(tr *Tr) error {
	if err := tr.WriteString("42000"); err != nil {
		return err
	}
	if err := tr.WriteString("Syntax error in SQL statement"); err != nil {
		return err
	}
	if err := tr.WriteString("SELECT 1"); err != nil {
		return err
	}
	if err := tr.WriteInt32(42001); err != nil {
		return err
	}
	return tr.WriteString("")
}

// TestFetchRowsColumnFailureMarksSessionDead verifies the core discard rule:
// a mid-column parse failure marks the session dead before the sticky error is
// returned; Next keeps returning it without I/O; Close performs no transport
// writes and releases cleanly.
func TestFetchRowsColumnFailureMarksSessionDead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	srvTr := NewReadWriter(serverConn)
	go func() {
		_ = serverConn.SetDeadline(time.Now().Add(time.Second))
		// Consume the RESULT_FETCH_ROWS request (op . resultID . fetchSize)
		// so the client's flush completes.
		_, _ = srvTr.ReadInt32()
		_, _ = srvTr.ReadInt32()
		_, _ = srvTr.ReadInt32()
		// Respond STATUS_OK, then a row whose column has a garbage type code.
		_ = srvTr.WriteInt32(StatusOK)
		_ = srvTr.WriteByte(1)
		_ = srvTr.WriteInt32(9999)
		_ = srvTr.Flush()
	}()

	sess := &Session{tr: NewReadWriter(clientConn)}
	rows := newDiscardTestRows(sess)
	dest := make([]driver.Value, 1)

	err := rows.Next(dest)
	if err == nil {
		t.Fatal("expected decode failure, got nil")
	}
	if !sess.dead.Load() {
		t.Error("session not marked dead after mid-column failure")
	}
	if sess.tr != nil {
		t.Error("transport not aborted after mid-column failure")
	}

	// Sticky error: a second Next must return the same error with no I/O.
	err2 := rows.Next(dest)
	if !errors.Is(err2, err) && err2.Error() != err.Error() {
		t.Errorf("sticky error changed: %v -> %v", err, err2)
	}

	// Close must skip transport writes entirely and return cleanly.
	if err := rows.Close(); err != nil {
		t.Errorf("Close after dead session: %v", err)
	}
	if rows.bufferedRows != nil {
		t.Error("Close should clear buffered rows")
	}
}

// TestFetchMoreRowsTransportFailureMarksSessionDead covers the request side:
// when the peer is gone, writing/flushing RESULT_FETCH_ROWS fails and the
// session must be marked dead as well.
func TestFetchMoreRowsTransportFailureMarksSessionDead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	// Peer disappears before any request is made.
	_ = serverConn.Close()

	sess := &Session{tr: NewReadWriter(clientConn)}
	rows := newDiscardTestRows(sess)
	dest := make([]driver.Value, 1)

	if err := rows.Next(dest); err == nil {
		t.Fatal("expected transport failure, got nil")
	}
	if !sess.dead.Load() {
		t.Error("session not marked dead after fetchMoreRows failure")
	}
}

// TestFetchRowsAlignedH2ErrorKeepsSessionAlive pins the aligned exception:
// a fully parsed server-side SQL error ends the result but NOT the session.
func TestFetchRowsAlignedH2ErrorKeepsSessionAlive(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	srvTr := NewReadWriter(serverConn)
	go func() {
		_ = serverConn.SetDeadline(time.Now().Add(time.Second))
		// Consume the RESULT_FETCH_ROWS request.
		_, _ = srvTr.ReadInt32()
		_, _ = srvTr.ReadInt32()
		_, _ = srvTr.ReadInt32()
		// STATUS_OK, then row flag -1 (exception) plus a complete frame.
		_ = srvTr.WriteInt32(StatusOK)
		_ = srvTr.WriteByte(255) // row flag -1: exception follows
		if err := writeH2ErrorFrameTr(srvTr); err != nil {
			t.Errorf("mock server: %v", err)
		}
		_ = srvTr.Flush()
	}()

	sess := &Session{tr: NewReadWriter(clientConn)}
	rows := newDiscardTestRows(sess)
	dest := make([]driver.Value, 1)

	err := rows.Next(dest)
	if err == nil {
		t.Fatal("expected the server-side error, got nil")
	}
	var h2Err *Error
	if !errors.As(err, &h2Err) {
		t.Fatalf("error %v is not an *h2go.Error", err)
	}
	if h2Err.Code != 42001 {
		t.Errorf("error code = %d, want 42001", h2Err.Code)
	}
	if sess.dead.Load() {
		t.Error("aligned H2 error frame must NOT mark the session dead")
	}
	if sess.tr == nil {
		t.Error("aligned H2 error frame must NOT abort the transport")
	}
}

// TestReadGeneratedKeysParseFailureMarksDead covers the generated-keys path:
// garbage metadata marks the session dead.
func TestReadGeneratedKeysParseFailureMarksDead(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, 1) // columnCount
	writeInt64(buf, 1) // rowCount
	// Truncated metadata: declared alias length with no payload.
	writeInt32(buf, 0x7FFFFFFF)

	sess := &Session{tr: mockTransferFromBytes(buf.Bytes())}
	_, _, err := sess.readGeneratedKeys()
	if err == nil {
		t.Fatal("expected metadata parse failure, got nil")
	}
	if !sess.dead.Load() {
		t.Error("session not marked dead after generated-keys metadata failure")
	}
}

// TestReadGeneratedKeysAlignedH2ErrorKeepsSessionAlive verifies that a fully
// parsed server-side error inside the keys frame keeps the session usable.
func TestReadGeneratedKeysAlignedH2ErrorKeepsSessionAlive(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, 1) // columnCount
	writeInt64(buf, 1) // rowCount
	// Valid single-column metadata (TIInteger carries no extra fields).
	writeString(buf, "ID") // alias
	writeString(buf, "")   // schema
	writeString(buf, "")   // table
	writeString(buf, "ID") // column name
	writeInt32(buf, TIInteger)
	buf.WriteByte(0) // identity=false
	writeInt32(buf, 2)

	// Row flag -1 plus a complete exception frame.
	buf.WriteByte(255)
	writeString(buf, "42000")
	writeString(buf, "Statement was cancelled")
	writeString(buf, "INSERT")
	writeInt32(buf, 57014)
	writeString(buf, "")

	// version must be protocol 21 so metadata framing matches (no legacy
	// displaySize field).
	sess := &Session{tr: mockTransferFromBytes(buf.Bytes()), version: TCPProtocolVersion21}
	_, _, err := sess.readGeneratedKeys()
	if err == nil {
		t.Fatal("expected the server-side error, got nil")
	}
	var h2Err *Error
	if !errors.As(err, &h2Err) {
		t.Fatalf("error %v is not an *h2go.Error", err)
	}
	if sess.dead.Load() {
		t.Error("aligned H2 error frame must NOT mark the session dead")
	}
}

// TestResetSessionRejectsDeadSession ties the discard rule together at the
// database/sql boundary: a marked-dead session fails ResetSession with
// driver.ErrBadConn so the pool discards the connection.
func TestResetSessionRejectsDeadSession(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	sess := &Session{tr: NewReadWriter(clientConn)}
	c := &conn{sess: sess}

	sess.markStreamBroken()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.ResetSession(ctx); !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("ResetSession after markStreamBroken = %v, want driver.ErrBadConn", err)
	}
}
