package h2go

// Session holds an authenticated H2 TCP session.
type Session struct {
	tr         *Tr
	id         string
	version    int32
	autoCommit bool
}

// Close closes the session and its underlying connection.
func (s *Session) Close() error {
	if s.tr != nil {
		return s.tr.Close()
	}
	return nil
}
