// command_test.go — Command preparation tests (scripted server responses).

package h2go

import (
	"bytes"
	"context"
	"testing"
)

// mockTransferFromBytes creates a Tr that reads from the provided bytes.
func mockTransferFromBytes(data []byte) *Tr {
	return NewReader(bytes.NewReader(data))
}

// TestPreparedCommand_CommandTypeName tests command type name mapping.
func TestPreparedCommand_CommandTypeName(t *testing.T) {
	tests := []struct {
		cmdType int32
		want    string
	}{
		{CmdUnknown, "UNKNOWN"},
		{CmdSelect, "SELECT"},
		{CmdInsert, "INSERT"},
		{CmdUpdate, "UPDATE"},
		{CmdDelete, "DELETE"},
		{CmdCreateTable, "CREATE TABLE"},
		{CmdCommit, "COMMIT"},
		{CmdRollback, "ROLLBACK"},
		{CmdMerge, "MERGE"},
		{999, "UNKNOWN(999)"},
	}

	for _, tc := range tests {
		got := CommandTypeName(tc.cmdType)
		if got != tc.want {
			t.Errorf("CommandTypeName(%d): got %q, want %q", tc.cmdType, got, tc.want)
		}
	}
}

// TestSession_NextCommandID tests command ID generation.
func TestSession_NextCommandID(t *testing.T) {
	s := &Session{}

	id1 := s.nextCommandID()
	id2 := s.nextCommandID()
	id3 := s.nextCommandID()

	if id1 != 1 {
		t.Errorf("first ID: got %d, want 1", id1)
	}
	if id2 != 2 {
		t.Errorf("second ID: got %d, want 2", id2)
	}
	if id3 != 3 {
		t.Errorf("third ID: got %d, want 3", id3)
	}
}

// TestSession_NextCommandID_Concurrent tests thread-safe ID generation.
func TestSession_NextCommandID_Concurrent(t *testing.T) {
	s := &Session{}

	// Generate IDs concurrently
	const numGoroutines = 10
	const idsPerGoroutine = 100

	results := make(chan int32, numGoroutines*idsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < idsPerGoroutine; j++ {
				results <- s.nextCommandID()
			}
		}()
	}

	// Collect all IDs
	ids := make(map[int32]bool)
	for i := 0; i < numGoroutines*idsPerGoroutine; i++ {
		id := <-results
		if ids[id] {
			t.Fatalf("Duplicate ID: %d", id)
		}
		ids[id] = true
	}

	// Verify we got sequential IDs from 1 to N
	if len(ids) != numGoroutines*idsPerGoroutine {
		t.Errorf("Got %d unique IDs, want %d", len(ids), numGoroutines*idsPerGoroutine)
	}
}

// TestPreparedCommand_Fields tests PreparedCommand struct fields.
func TestPreparedCommand_Fields(t *testing.T) {
	cmd := &PreparedCommand{
		ID:         42,
		SQL:        "SELECT * FROM users WHERE id = ?",
		IsQuery:    true,
		ReadOnly:   true,
		CmdType:    CmdSelect,
		ParamCount: 1,
		Params: []ParameterMeta{
			{Index: 0, TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
		},
	}

	if cmd.ID != 42 {
		t.Errorf("ID: got %d, want 42", cmd.ID)
	}
	if cmd.SQL != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("SQL: got %q, want %q", cmd.SQL, "SELECT * FROM users WHERE id = ?")
	}
	if !cmd.IsQuery {
		t.Error("IsQuery: expected true")
	}
	if !cmd.ReadOnly {
		t.Error("ReadOnly: expected true")
	}
	if cmd.CmdType != CmdSelect {
		t.Errorf("CmdType: got %d, want %d", cmd.CmdType, CmdSelect)
	}
	if cmd.ParamCount != 1 {
		t.Errorf("ParamCount: got %d, want 1", cmd.ParamCount)
	}
	if len(cmd.Params) != 1 {
		t.Errorf("len(Params): got %d, want 1", len(cmd.Params))
	}
}

// TestSession_PrepareCommand_ClosedSession tests error on closed session.
func TestSession_PrepareCommand_ClosedSession(t *testing.T) {
	s := &Session{tr: nil}

	_, err := s.PrepareCommand(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("Expected error for closed session")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("closed")) {
		t.Errorf("Error message should mention 'closed', got: %v", err)
	}
}

// TestSession_PrepareCommandReadParams_ClosedSession tests error on closed session.
func TestSession_PrepareCommandReadParams_ClosedSession(t *testing.T) {
	s := &Session{tr: nil}

	_, err := s.PrepareCommandReadParams(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("Expected error for closed session")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("closed")) {
		t.Errorf("Error message should mention 'closed', got: %v", err)
	}
}

// TestPreparedCommand_Close_NilSession handles closing without a valid session.
func TestPreparedCommand_Close_NilSession(t *testing.T) {
	cmd := &PreparedCommand{ID: 1}

	// Should not panic or error
	err := cmd.Close(nil)
	if err != nil {
		t.Errorf("Close(nil): unexpected error: %v", err)
	}
}

// TestCommandConstants tests that command type constants have expected values.
func TestCommandConstants(t *testing.T) {
	// These values are from CommandRemote.java
	if CmdUnknown != 0 {
		t.Errorf("CmdUnknown: got %d, want 0", CmdUnknown)
	}
	if CmdSelect != 1 {
		t.Errorf("CmdSelect: got %d, want 1", CmdSelect)
	}
	if CmdInsert != 2 {
		t.Errorf("CmdInsert: got %d, want 2", CmdInsert)
	}
	if CmdUpdate != 3 {
		t.Errorf("CmdUpdate: got %d, want 3", CmdUpdate)
	}
	if CmdDelete != 4 {
		t.Errorf("CmdDelete: got %d, want 4", CmdDelete)
	}
	if CmdCommit != 12 {
		t.Errorf("CmdCommit: got %d, want 12", CmdCommit)
	}
	if CmdRollback != 13 {
		t.Errorf("CmdRollback: got %d, want 13", CmdRollback)
	}
	if CmdMerge != 22 {
		t.Errorf("CmdMerge: got %d, want 22", CmdMerge)
	}
}

// TestParameterMeta_Fields tests ParameterMeta struct.
func TestParameterMeta_Fields(t *testing.T) {
	pm := ParameterMeta{
		Index: 0,
		TypeInfo: &TypeInfo{
			ValueType: ValueTypeBigint,
			Precision: 19,
		},
	}

	if pm.Index != 0 {
		t.Errorf("Index: got %d, want 0", pm.Index)
	}
	if pm.TypeInfo.ValueType != ValueTypeBigint {
		t.Errorf("TypeInfo.ValueType: got %d, want %d", pm.TypeInfo.ValueType, ValueTypeBigint)
	}
}

// TestPreparedCommand_WithParams tests a command with multiple parameters.
func TestPreparedCommand_WithParams(t *testing.T) {
	params := []ParameterMeta{
		{Index: 0, TypeInfo: &TypeInfo{ValueType: ValueTypeInteger}},
		{Index: 1, TypeInfo: &TypeInfo{ValueType: ValueTypeVarchar, Precision: 100}},
		{Index: 2, TypeInfo: &TypeInfo{ValueType: ValueTypeDouble}},
	}

	cmd := &PreparedCommand{
		ID:         7,
		SQL:        "SELECT * FROM t WHERE a = ? AND b = ? AND c > ?",
		IsQuery:    true,
		ReadOnly:   true,
		CmdType:    CmdSelect,
		ParamCount: 3,
		Params:     params,
	}

	if len(cmd.Params) != 3 {
		t.Fatalf("len(Params): got %d, want 3", len(cmd.Params))
	}

	// Check parameter indices
	for i, p := range cmd.Params {
		if p.Index != i {
			t.Errorf("Params[%d].Index: got %d, want %d", i, p.Index, i)
		}
	}

	// Check types
	if cmd.Params[0].TypeInfo.ValueType != ValueTypeInteger {
		t.Errorf("Params[0] type: got %d, want INTEGER", cmd.Params[0].TypeInfo.ValueType)
	}
	if cmd.Params[1].TypeInfo.ValueType != ValueTypeVarchar {
		t.Errorf("Params[1] type: got %d, want VARCHAR", cmd.Params[1].TypeInfo.ValueType)
	}
	if cmd.Params[2].TypeInfo.ValueType != ValueTypeDouble {
		t.Errorf("Params[2] type: got %d, want DOUBLE", cmd.Params[2].TypeInfo.ValueType)
	}
}

// Helper function for byte slice contains
type myBytes []byte

func (b myBytes) Contains(sub []byte) bool {
	return bytes.Contains(b, sub)
}
