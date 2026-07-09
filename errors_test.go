package h2go

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReadH2Error(t *testing.T) {
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	if err := tr.WriteString("22012"); err != nil {
		t.Fatalf("WriteString(sqlState): %v", err)
	}
	if err := tr.WriteString("division by zero"); err != nil {
		t.Fatalf("WriteString(message): %v", err)
	}
	if err := tr.WriteString("SELECT 1/0"); err != nil {
		t.Fatalf("WriteString(sql): %v", err)
	}
	if err := tr.WriteInt32(22012); err != nil {
		t.Fatalf("WriteInt32(code): %v", err)
	}
	if err := tr.WriteString("stack trace line 1\nstack trace line 2"); err != nil {
		t.Fatalf("WriteString(stackTrace): %v", err)
	}
	if err := tr.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	err := readH2Error(mockTransferFromBytes(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error")
	}
	h2err, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if h2err.SQLState != "22012" || h2err.Code != 22012 {
		t.Fatalf("decoded fields mismatch: %+v", h2err)
	}
	if h2err.Message != "division by zero" || h2err.SQL != "SELECT 1/0" {
		t.Fatalf("decoded fields mismatch: %+v", h2err)
	}
	if !strings.Contains(h2err.Error(), "division by zero") || !strings.Contains(h2err.Error(), "22012") || !strings.Contains(h2err.Error(), "SELECT 1/0") {
		t.Fatalf("unexpected Error() string: %q", h2err.Error())
	}
	formatted := fmt.Sprintf("%+v", h2err)
	if !strings.Contains(formatted, "stack trace line 1") || !strings.Contains(formatted, "stack trace line 2") {
		t.Fatalf("expected stack trace in %%+v formatting, got %q", formatted)
	}
}

func TestReadStatusError_ReturnsH2Error(t *testing.T) {
	// Encode a STATUS_ERROR frame: status code + readH2Error payload.
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	_ = tr.WriteInt32(StatusError) // status
	_ = tr.WriteString("42000")    // sqlState
	_ = tr.WriteString("syntax error near TOKEN")
	_ = tr.WriteString("SELECT * FORM t") // sql (intentional typo)
	_ = tr.WriteInt32(42001)              // errorCode
	_ = tr.WriteString("stack")           // stackTrace
	_ = tr.Flush()

	err := readStatus(mockTransferFromBytes(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error")
	}

	var h2err *Error
	if !errors.As(err, &h2err) {
		t.Fatalf("expected *Error via errors.As, got %T: %v", err, err)
	}
	if h2err.SQLState != "42000" {
		t.Errorf("SQLState = %q, want 42000", h2err.SQLState)
	}
	if h2err.Code != 42001 {
		t.Errorf("Code = %d, want 42001", h2err.Code)
	}
	if !strings.Contains(h2err.Message, "syntax error") {
		t.Errorf("Message = %q, want contains 'syntax error'", h2err.Message)
	}
	// *Error must also be directly type-assertable (no extra wrapping).
	if _, ok := err.(*Error); !ok {
		t.Errorf("err.(*Error) failed; got %T — readStatus must return bare *Error for STATUS_ERROR", err)
	}
}

func TestReadStatusClosedReturnsErrClosed(t *testing.T) {
	var buf bytes.Buffer
	tr := NewWriter(&writeCloseBuffer{&buf})
	if err := tr.WriteInt32(StatusClosed); err != nil {
		t.Fatalf("WriteInt32: %v", err)
	}
	if err := tr.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	err := readStatus(mockTransferFromBytes(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %T: %v", err, err)
	}
}

func TestWrapErrorPreservesH2Error(t *testing.T) {
	want := &Error{SQLState: "42000", Message: "bad SQL", Code: 42000}
	got := wrapError("PrepareCommand", want)
	if got != want {
		t.Fatalf("wrapError returned %T(%v), want the original *Error", got, got)
	}
}

func TestSentinelErrors(t *testing.T) {
	for name, target := range map[string]error{
		"server version":   ErrUnsupportedServerVersion,
		"unsupported type": ErrUnsupportedType,
		"closed":           ErrClosed,
	} {
		t.Run(name, func(t *testing.T) {
			if target == nil {
				t.Fatal("sentinel is nil")
			}
			if !errors.Is(fmt.Errorf("wrap: %w", target), target) {
				t.Fatalf("errors.Is failed for %v", target)
			}
		})
	}
}
