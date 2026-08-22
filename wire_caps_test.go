// wire_caps_test.go — unit tests for wire-controlled length/count caps
// (MATURITY_ROUND_II_PLAN.md Task 4, findings 5 and 17).
//
// The oversized values here are kept just above their caps: if a guard were
// removed, the test would visibly OOM or hang on stream EOF rather than
// silently pass.

package h2go

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestReadArrayValueElementCountCap feeds an ARRAY frame claiming one element
// above the cap.
func TestReadArrayValueElementCountCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeArray)
	writeInt32(buf, maxWireCollectionElements+1)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, &lobCollector{})
	if err == nil {
		t.Fatal("expected cap error, got nil")
	}
	for _, want := range []string{"element count", "exceeds cap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestReadArrayValueLegacyNegativeCountCap covers the 1.4.200 legacy
// negative-length encoding: the complemented count must hit the same guard.
func TestReadArrayValueLegacyNegativeCountCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeArray)
	// ^raw == maxWireCollectionElements+1, i.e. raw = -(cap+2) in two's
	// complement; the decoder complements it back before guarding.
	writeInt32(buf, -(maxWireCollectionElements + 2))

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, &lobCollector{})
	if err == nil {
		t.Fatal("expected cap error after complementing legacy length, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Errorf("error %q should mention the cap", err)
	}
}

// TestReadArrayValueLegacySkipErrorReturned pins finding 17: when skipping the
// legacy type-name string fails, that error is returned (previously it was
// discarded with `_, _ =` and the decode continued from a misaligned stream).
func TestReadArrayValueLegacySkipErrorReturned(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeArray)
	writeInt32(buf, ^int32(1)) // legacy negative; complement is 1 element
	// Corrupt type-name string: huge declared length, no payload bytes.
	writeInt32(buf, 0x7FFFFFFF)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, &lobCollector{})
	if err == nil {
		t.Fatal("expected the skip-path error to be returned, got nil")
	}
	if !strings.Contains(err.Error(), "skip legacy type name") {
		t.Errorf("error %q should name the skip path", err)
	}
}

// TestReadRowValueFieldCountCap feeds a ROW frame claiming one field above the
// cap.
func TestReadRowValueFieldCountCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeRow)
	writeInt32(buf, maxWireCollectionElements+1)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, nil)
	if err == nil {
		t.Fatal("expected cap error, got nil")
	}
	for _, want := range []string{"field count", "exceeds cap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestReadRowValueNegativeCountRejected pins that a negative ROW field count —
// which the reference reader never writes — is rejected as a broken frame
// instead of being complemented like legacy ARRAY lengths.
func TestReadRowValueNegativeCountRejected(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeRow)
	writeInt32(buf, -5)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, nil)
	if err == nil {
		t.Fatal("expected invalid-count error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid field count") {
		t.Errorf("error %q should name the invalid count", err)
	}
}

// TestReadInlineBlobLengthCap feeds an inline BLOB frame whose length exceeds
// MaxWireLength; the guard must fire before make([]byte, length).
func TestReadInlineBlobLengthCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeBlob)
	writeInt64(buf, MaxWireLength+1)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, nil)
	if err == nil {
		t.Fatal("expected wire-cap error, got nil")
	}
	for _, want := range []string{"inline BLOB length", "wire cap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestReadInlineClobCharLengthCap feeds an inline CLOB frame whose char length
// exceeds maxInlineClobChars.
func TestReadInlineClobCharLengthCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeClob)
	writeInt64(buf, maxInlineClobChars+1)

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.readValueInternal(nil, nil)
	if err == nil {
		t.Fatal("expected cap error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Errorf("error %q should mention the cap", err)
	}
}

// TestReadGeneratedKeysRowCountCap feeds a generated-keys frame with a row
// count above the cap; the guard must fire before pre-allocating result.Rows.
func TestReadGeneratedKeysRowCountCap(t *testing.T) {
	buf := new(bytes.Buffer)
	writeInt32(buf, 1) // columnCount
	writeRowCountForTest(buf, maxWireCollectionElements+1)

	s := &Session{tr: mockTransferFromBytes(buf.Bytes())}
	_, _, err := s.readGeneratedKeys()
	if err == nil {
		t.Fatal("expected row-count cap error, got nil")
	}
	for _, want := range []string{"row count", "exceeds cap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// writeRowCountForTest writes an int64 row count (Tr.ReadRowCount reads a raw
// long).
func writeRowCountForTest(buf *bytes.Buffer, n int64) {
	writeInt64(buf, n)
}

// TestFetchLobChunkLengthGuard verifies fetchLob rejects a LOB_READ response
// claiming more bytes than the requested chunk size instead of allocating from
// it.
func TestFetchLobChunkLengthGuard(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	srvTr := NewReadWriter(serverConn)
	go func() {
		_ = serverConn.SetDeadline(time.Now().Add(time.Second))
		// Read the LOB_READ request: op . lobID . hmac . offset . length.
		_, _ = srvTr.ReadInt32()
		_, _ = srvTr.ReadInt64()
		_, _ = srvTr.ReadBytes()
		_, _ = srvTr.ReadInt64()
		_, _ = srvTr.ReadInt32()
		// Claim one byte more than the client requested: protocol violation.
		_ = srvTr.WriteInt32(StatusOK)
		_ = srvTr.WriteInt32(lobReadChunkSize + 1)
		_ = srvTr.Flush()
	}()

	clientTr := NewReadWriter(clientConn)
	if err := clientTr.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	p := &pendingLob{typeCode: ValueTypeBlob, lobID: 42, hmac: []byte{0x01}, precision: 100}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- func() error { _, err := clientTr.fetchLob(p); return err }() }()

	select {
	case <-ctx.Done():
		t.Fatal("fetchLob did not return before deadline")
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected chunk-length error, got nil")
		}
		if !strings.Contains(err.Error(), "outside expected range") {
			t.Errorf("error %q should name the violated range", err)
		}
	}
}

// Regression: normal small collections still decode exactly as before.
func TestSmallCollectionsStillDecode(t *testing.T) {
	// ARRAY of two integers.
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeArray)
	writeInt32(buf, 2)
	writeValueType(buf, ValueTypeInteger)
	writeInt32(buf, 7)
	writeValueType(buf, ValueTypeInteger)
	writeInt32(buf, -9)

	tr := mockTransferFromBytes(buf.Bytes())
	got, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("array decode: %v", err)
	}
	if got != "[7,-9]" {
		t.Errorf("array = %v, want [7,-9]", got)
	}

	// ROW of two strings.
	buf2 := new(bytes.Buffer)
	writeValueType(buf2, ValueTypeRow)
	writeInt32(buf2, 2)
	writeValueType(buf2, ValueTypeVarchar)
	writeString(buf2, "a")
	writeValueType(buf2, ValueTypeVarchar)
	writeString(buf2, "b")

	tr2 := mockTransferFromBytes(buf2.Bytes())
	got2, err := tr2.ReadValue(nil)
	if err != nil {
		t.Fatalf("row decode: %v", err)
	}
	if got2 != "(a,b)" {
		t.Errorf("row = %v, want (a,b)", got2)
	}

	// Legacy-negative ARRAY with type name and one element.
	buf3 := new(bytes.Buffer)
	writeValueType(buf3, ValueTypeArray)
	writeInt32(buf3, ^int32(1)) // complement is 1 element
	writeString(buf3, "INTEGER")
	writeValueType(buf3, ValueTypeInteger)
	writeInt32(buf3, 7)

	tr3 := mockTransferFromBytes(buf3.Bytes())
	got3, err := tr3.ReadValue(nil)
	if err != nil {
		t.Fatalf("legacy array decode: %v", err)
	}
	if got3 != "[7]" {
		t.Errorf("legacy array = %v, want [7]", got3)
	}
}
