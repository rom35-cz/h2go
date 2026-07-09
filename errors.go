package h2go

import (
	"errors"
	"fmt"
)

// Error represents a structured error returned by the H2 server.
//
// H2 server errors carry SQLState, vendor code, message, SQL text, and a stack
// trace. The default Error() string keeps the trace hidden; use %+v to include
// it when debugging.
type Error struct {
	SQLState   string
	Message    string
	SQL        string
	Code       int32
	StackTrace string
}

// H2Error is kept as a compatibility alias for older code and tests.
type H2Error = Error

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "h2go: <nil> H2 error"
	}
	msg := "h2go: H2 error"
	if e.SQLState != "" || e.Code != 0 {
		msg += fmt.Sprintf(" [%s/%d]", e.SQLState, e.Code)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.SQL != "" {
		msg += fmt.Sprintf(" (sql=%q)", e.SQL)
	}
	return msg
}

// Format implements fmt.Formatter. With %+v, the server stack trace is
// appended after the concise error string.
func (e *Error) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v', 's':
		if e == nil {
			_, _ = fmt.Fprint(f, "<nil>")
			return
		}
		_, _ = fmt.Fprint(f, e.Error())
		if verb == 'v' && f.Flag('+') && e.StackTrace != "" {
			_, _ = fmt.Fprint(f, "\n", e.StackTrace)
		}
	case 'q':
		if e == nil {
			_, _ = fmt.Fprint(f, "<nil>")
			return
		}
		_, _ = fmt.Fprintf(f, "%q", e.Error())
	}
}

// readH2Error reads a STATUS_ERROR response from the H2 server.
// Wire format (Java SessionRemote.readSQLException):
//
//	sqlState   (string)
//	message    (string)
//	sql        (string, may be null)
//	errorCode  (int32)
//	stackTrace (string)
//
// If any field cannot be read (e.g. connection dropped mid-response) the I/O
// error is returned directly instead of a partial H2Error.
func readH2Error(tr *Tr) error {
	sqlState, err := tr.ReadString()
	if err != nil {
		return fmt.Errorf("h2go: read error response (sqlState): %w", err)
	}
	message, err := tr.ReadString()
	if err != nil {
		return fmt.Errorf("h2go: read error response (message): %w", err)
	}
	sql, err := tr.ReadString()
	if err != nil {
		return fmt.Errorf("h2go: read error response (sql): %w", err)
	}
	errorCode, err := tr.ReadInt32()
	if err != nil {
		return fmt.Errorf("h2go: read error response (errorCode): %w", err)
	}
	stackTrace, err := tr.ReadString()
	if err != nil {
		return fmt.Errorf("h2go: read error response (stackTrace): %w", err)
	}
	return &Error{
		SQLState:   derefStr(sqlState),
		Message:    derefStr(message),
		SQL:        derefStr(sql),
		Code:       errorCode,
		StackTrace: derefStr(stackTrace),
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// readStatus reads a status int from the server and returns nil on success
// (STATUS_OK or STATUS_OK_STATE_CHANGED), an *Error on STATUS_ERROR, or a
// plain error for STATUS_CLOSED or unexpected codes. This mirrors the Java
// SessionRemote.done() method.
func readStatus(tr *Tr) error {
	status, err := tr.ReadInt32()
	if err != nil {
		return fmt.Errorf("h2go: read status: %w", err)
	}
	switch status {
	case StatusOK, StatusOKStateChanged:
		return nil
	case StatusError:
		return readH2Error(tr)
	case StatusClosed:
		return fmt.Errorf("h2go: server closed the session: %w", ErrClosed)
	default:
		return fmt.Errorf("h2go: unexpected status code %d", status)
	}
}

// wrapError wraps an underlying error with context about the operation.
// If the error is already an H2 server error, it is returned unchanged so
// callers can type-assert to *Error / *H2Error directly.
func wrapError(op string, err any) error {
	if e, ok := err.(error); ok {
		var h2Err *Error
		if errors.As(e, &h2Err) {
			return h2Err
		}
		return fmt.Errorf("h2go: %s: %w", op, e)
	}
	return fmt.Errorf("h2go: %s: %v", op, err)
}

var (
	// ErrUnsupportedServerVersion reports that the server did not negotiate the
	// requested H2 protocol version.
	ErrUnsupportedServerVersion = errors.New("unsupported H2 server version")
	// ErrUnsupportedType reports an H2 type that the MVP driver cannot decode
	// or encode yet.
	ErrUnsupportedType = errors.New("unsupported H2 type")
	// ErrClosed reports that the server/session connection has been closed.
	ErrClosed = errors.New("connection closed")
	// ErrLastInsertIDUnavailable reports that a statement did not return a
	// single numeric generated key suitable for driver.Result.LastInsertId().
	ErrLastInsertIDUnavailable = errors.New("last insert id unavailable")
)
