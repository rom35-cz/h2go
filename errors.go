package h2go

import (
	"fmt"
	"os"
	"time"
)

// H2Error represents a structured error returned by the H2 server.
// Phase 7 will add full decoding; for now it captures the fields sent
// in the STATUS_ERROR response.
type H2Error struct {
	SQLState   string
	Message    string
	SQL        string
	Code       int32
	StackTrace string
}

// Error implements the error interface.
func (e *H2Error) Error() string {
	return fmt.Sprintf("H2 error [%s] %d: %s", e.SQLState, e.Code, e.Message)
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
	return &H2Error{
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
// (STATUS_OK or STATUS_OK_STATE_CHANGED), an *H2Error on STATUS_ERROR, or
// a plain error for STATUS_CLOSED or unexpected codes.
// This mirrors the Java SessionRemote.done() method.
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
		return fmt.Errorf("h2go: server closed the session")
	default:
		return fmt.Errorf("h2go: unexpected status code %d", status)
	}
}

// wrapError wraps an underlying error with context about the operation.
// If err is already a string (message-only), it creates a new error.
func wrapError(op string, err any) error {
	if e, ok := err.(error); ok {
		return fmt.Errorf("h2go: %s: %w", op, e)
	}
	return fmt.Errorf("h2go: %s: %v", op, err)
}

// localTimeZoneID returns the system's local timezone identifier.
// It first checks the TZ environment variable, then falls back to the
// Go runtime's local timezone name, and ultimately to "UTC".
func localTimeZoneID() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if name := time.Local.String(); name != "Local" {
		return name
	}
	return "UTC"
}
