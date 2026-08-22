// lob_test.go — unit tests for deferred fetch-on-demand LOB resolution at
// batch boundaries (MATURITY_ROUND_II_PLAN.md Task 1, finding 1).

package h2go

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"
)

// writeOnDemandClobFrameBuf appends a fetch-on-demand CLOB value frame
// (Transfer.readValue CLOB case, length == -1) to a buffer.
func writeOnDemandClobFrameBuf(buf *bytes.Buffer, lobID int64, hmac []byte, octetLength, charLength int64) {
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, -1) // fetch-on-demand marker
	writeInt32(buf, 1)  // tableID (unused by the driver)
	writeInt64(buf, lobID)
	writeBytes(buf, hmac)
	writeInt64(buf, octetLength)
	writeInt64(buf, charLength)
}

// writeOnDemandClobFrameTr writes the same frame directly to a Tr.
func writeOnDemandClobFrameTr(tr *Tr, lobID int64, hmac []byte, octetLength, charLength int64) error {
	if err := tr.WriteInt32(ValueTypeClob); err != nil {
		return err
	}
	if err := tr.WriteInt64(-1); err != nil {
		return err
	}
	if err := tr.WriteInt32(1); err != nil { // tableID
		return err
	}
	if err := tr.WriteInt64(lobID); err != nil {
		return err
	}
	if err := tr.WriteBytes(hmac); err != nil {
		return err
	}
	if err := tr.WriteInt64(octetLength); err != nil {
		return err
	}
	return tr.WriteInt64(charLength)
}

// TestReadValueInternalDefersOnDemandLob verifies that a collector turns the
// fetch-on-demand branch into a placeholder instead of issuing LOB_READ
// requests, and that the stream stays aligned for subsequent values.
func TestReadValueInternalDefersOnDemandLob(t *testing.T) {
	buf := new(bytes.Buffer)
	writeOnDemandClobFrameBuf(buf, 77, []byte{0xAA, 0xBB}, 11, 11)
	// A scalar after the LOB: proves nothing beyond the LOB frame is consumed.
	writeValueType(buf, ValueTypeInteger)
	writeInt32(buf, 42)

	tr := mockTransferFromBytes(buf.Bytes())
	lc := &lobCollector{curRow: 2, curCol: 1}

	val, err := tr.readValueInternal(nil, lc)
	if err != nil {
		t.Fatalf("readValueInternal: %v", err)
	}
	p, ok := val.(*pendingLob)
	if !ok {
		t.Fatalf("expected *pendingLob placeholder, got %T", val)
	}
	if p.typeCode != ValueTypeClob || p.lobID != 77 {
		t.Errorf("placeholder identity: typeCode=%d lobID=%d, want %d/77", p.typeCode, p.lobID, ValueTypeClob)
	}
	if !bytes.Equal(p.hmac, []byte{0xAA, 0xBB}) {
		t.Errorf("hmac = %v", p.hmac)
	}
	if p.octetLength != 11 || p.precision != 11 {
		t.Errorf("lengths = %d/%d, want 11/11", p.octetLength, p.precision)
	}
	if p.row != 2 || p.col != 1 {
		t.Errorf("position = (%d,%d), want (2,1)", p.row, p.col)
	}
	if len(lc.pending) != 1 || lc.pending[0] != p {
		t.Fatalf("collector recorded %d placeholders, want exactly this one", len(lc.pending))
	}

	// The following value must decode untouched.
	got, err := tr.readValueInternal(nil, nil)
	if err != nil {
		t.Fatalf("following value: %v", err)
	}
	if got != int64(42) {
		t.Errorf("following value = %v, want 42 (stream desynchronized)", got)
	}
}

// TestReadValueNilCollectorFetchesImmediately keeps the legacy standalone
// behaviour pinned: without a collector the on-demand branch issues LOB_READ
// writes, which fail on a read-only Tr rather than silently misreading the
// stream.
func TestReadValueNilCollectorFetchesImmediately(t *testing.T) {
	buf := new(bytes.Buffer)
	writeOnDemandClobFrameBuf(buf, 5, []byte{0x01}, 3, 3)

	tr := mockTransferFromBytes(buf.Bytes()) // read-only: no writer side
	if _, err := tr.ReadValue(nil); err == nil {
		t.Fatal("expected an error when fetching on-demand with no writer side, got nil")
	}
}

// serveOneLobRead answers one LOB_READ request using the server's framing:
// request op . lobId . hmac . offset . length;
// response status . actualLen . <actualLen raw bytes>.
// An empty data value answers with a zero-length chunk, which is how the
// server signals end-of-LOB and terminates the client's chunk loop.
// Framed values go through srvTr's buffered writer; the raw chunk payload goes
// directly over the connection after a flush to preserve ordering.
func serveOneLobRead(srvTr *Tr, raw net.Conn, data string) error {
	op, err := srvTr.ReadInt32()
	if err != nil {
		return err
	}
	if op != LobRead {
		return errors.New("mock server: unexpected op")
	}
	if _, err := srvTr.ReadInt64(); err != nil { // lobID
		return err
	}
	if _, err := srvTr.ReadBytes(); err != nil { // hmac
		return err
	}
	if _, err := srvTr.ReadInt64(); err != nil { // offset
		return err
	}
	if _, err := srvTr.ReadInt32(); err != nil { // requested length
		return err
	}
	if err := srvTr.WriteInt32(StatusOK); err != nil {
		return err
	}
	if err := srvTr.WriteInt32(int32(len(data))); err != nil {
		return err
	}
	if err := srvTr.Flush(); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil // EOF chunk: no payload bytes follow
	}
	_, err = raw.Write([]byte(data))
	return err
}

// TestFetchRowsResolvesDeferredLobsAtBatchBoundary reproduces finding 1 at the
// unit level: two rows, each with an INTEGER followed by a fetch-on-demand
// CLOB, plus the end-of-result flag. Before the fix, parsing row 0 column 1
// issued a mid-batch LOB_READ and then misread row 1's flag byte as a status.
func TestFetchRowsResolvesDeferredLobsAtBatchBoundary(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	srvTr := NewReadWriter(serverConn)
	go func() {
		// Batch: row0 = (1, LOB "alpha"), row1 = (2, LOB "beta!"), terminator.
		_ = srvTr.WriteByte(1)
		_ = srvTr.WriteInt32(ValueTypeInteger)
		_ = srvTr.WriteInt32(1)
		_ = writeOnDemandClobFrameTr(srvTr, 100, []byte{0x01}, 5, 5)
		_ = srvTr.WriteByte(1)
		_ = srvTr.WriteInt32(ValueTypeInteger)
		_ = srvTr.WriteInt32(2)
		_ = writeOnDemandClobFrameTr(srvTr, 200, []byte{0x02}, 6, 6)
		_ = srvTr.WriteByte(0)
		_ = srvTr.Flush()

		// Only now are the deferred LOB_READ requests serviced. Each LOB needs
		// a data chunk plus a zero-length terminator. Errors are deliberately
		// ignored here; the client-side assertions below are authoritative and
		// fail loudly if the mock misbehaves.
		_ = serveOneLobRead(srvTr, serverConn, "alpha")
		_ = serveOneLobRead(srvTr, serverConn, "")
		_ = serveOneLobRead(srvTr, serverConn, "beta!")
		_ = serveOneLobRead(srvTr, serverConn, "")
	}()

	clientTr := NewReadWriter(clientConn)
	r := &Rows{
		session:     &Session{tr: clientTr},
		columnCount: 2,
		columns: &ResultMeta{
			ColumnCount: 2,
			Columns:     []ResultColumn{{Alias: "i"}, {Alias: "c"}},
		},
		fetchSize: 10,
	}

	if err := r.fetchRows(10); err != nil {
		t.Fatalf("fetchRows: %v", err)
	}
	if !r.noMoreRows {
		t.Error("noMoreRows not set despite end-of-result flag")
	}
	if len(r.bufferedRows) != 2 {
		t.Fatalf("buffered %d rows, want 2", len(r.bufferedRows))
	}
	if r.bufferedRows[0][0] != int64(1) || r.bufferedRows[1][0] != int64(2) {
		t.Errorf("integer columns desynchronized: %v / %v", r.bufferedRows[0][0], r.bufferedRows[1][0])
	}
	if s, ok := r.bufferedRows[0][1].(string); !ok || s != "alpha" {
		t.Errorf("row 0 col 1 = %#v, want \"alpha\"", r.bufferedRows[0][1])
	}
	if s, ok := r.bufferedRows[1][1].(string); !ok || s != "beta!" {
		t.Errorf("row 1 col 1 = %#v, want \"beta!\"", r.bufferedRows[1][1])
	}
}

// TestArrayWithNestedOnDemandLobRejected pins the documented limitation: a
// fetch-on-demand LOB nested inside ARRAY decodes to ErrUnsupportedType and is
// recognizable as errNestedOnDemandLOB by session-abort handling.
func TestArrayWithNestedOnDemandLobRejected(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeArray)
	writeInt32(buf, 1) // one element
	writeOnDemandClobFrameBuf(buf, 9, []byte{0x07}, 4, 4)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(&TypeInfo{}, &lobCollector{})
	if err == nil {
		t.Fatal("expected ErrUnsupportedType for nested on-demand LOB, got nil")
	}
	if !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("error should wrap ErrUnsupportedType, got: %v", err)
	}
	if !errors.Is(err, errNestedOnDemandLOB) {
		t.Errorf("error should wrap errNestedOnDemandLOB, got: %v", err)
	}
}

// TestRowWithNestedOnDemandLobRejected covers the ROW container variant.
func TestRowWithNestedOnDemandLobRejected(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeRow)
	writeInt32(buf, 1) // one field
	writeOnDemandClobFrameBuf(buf, 9, []byte{0x07}, 4, 4)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, &lobCollector{})
	if err == nil {
		t.Fatal("expected ErrUnsupportedType for nested on-demand LOB, got nil")
	}
	if !errors.Is(err, errNestedOnDemandLOB) {
		t.Errorf("error should wrap errNestedOnDemandLOB, got: %v", err)
	}
}

// TestResolveFailureAbortsSession verifies that a failed deferred-LOB
// resolution marks the session dead (transport aborted), so ResetSession fails
// and database/sql discards the connection instead of reusing it.
func TestResolveFailureAbortsSession(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	srvTr := NewReadWriter(serverConn)
	go func() {
		// Batch with one deferred LOB plus the end-of-result flag; the mock
		// then never answers the LOB_READ request, so resolution must fail.
		_ = srvTr.WriteByte(1)
		_ = writeOnDemandClobFrameTr(srvTr, 300, nil, 3, 3)
		_ = srvTr.WriteByte(0)
		_ = srvTr.Flush()
		_ = serverConn.SetDeadline(time.Now().Add(time.Second))
	}()

	clientTr := NewReadWriter(clientConn)
	// Bound the blocked status read so resolution fails deterministically.
	if err := clientTr.SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	sess := &Session{tr: clientTr}
	r := &Rows{
		session:     sess,
		columnCount: 1,
		columns: &ResultMeta{
			ColumnCount: 1,
			Columns:     []ResultColumn{{Alias: "c"}},
		},
		fetchSize: 10,
	}

	err := r.fetchRows(10)
	if err == nil {
		t.Fatal("expected resolution failure, got nil")
	}
	if sess.tr != nil {
		t.Error("expected session transport to be aborted after resolution failure")
	}
	if !sess.dead.Load() {
		t.Error("expected session to be marked dead after resolution failure")
	}
}
