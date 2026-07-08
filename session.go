package h2go

import "sync"

// Session holds an authenticated H2 TCP session.
type Session struct {
	tr         *Tr
	id         string
	version    int32
	autoCommit bool
	mu         sync.Mutex // protects nextID
	nextID     int32      // incremented for each command ID
}

// Close sends a graceful SESSION_CLOSE notification to the server and closes
// the underlying TCP connection.
//
// Following the H2 TCP protocol, the client sends the SessionClose operation
// (op=1) and reads the server's STATUS_OK reply before closing the socket.
// This prevents the server from logging "Connection is broken" on every clean
// session termination.
//
// All intermediate errors (write, flush, read) are best-effort: they are
// discarded so that a broken-connection close still completes. Future phases
// may add a configurable deadline on the STATUS_OK read.
func (s *Session) Close() error {
	if s.tr == nil {
		return nil
	}
	tr := s.tr
	s.tr = nil // prevent double-close

	// Notify the server and drain its STATUS_OK reply. Errors are discarded:
	// the transport must be closed regardless of the session state.
	_ = tr.WriteInt32(SessionClose)
	_ = tr.Flush()
	_, _ = tr.ReadInt32() // STATUS_OK; ignore value and error

	return tr.Close()
}
