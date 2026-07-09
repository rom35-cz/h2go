package h2go

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
)

// mockServer starts a TCP listener on a random local port, runs serverFn in a
// goroutine for each accepted connection, and registers a cleanup that waits
// for the goroutine to finish before the test ends.
//
// Using t.Cleanup + a done channel ensures that any t.Errorf / t.Logf calls
// inside serverFn complete before the test is marked finished, preventing
// the "testing: t.Errorf called after test finished" panic.
func mockServer(t *testing.T, serverFn func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed before a connection arrived
		}
		defer conn.Close()
		serverFn(conn)
	}()
	t.Cleanup(func() {
		ln.Close() // unblock Accept if the goroutine is still waiting
		<-done     // wait for serverFn to return before the test ends
	})
	return ln.Addr().String()
}

func parseAddr(t *testing.T, addr string) (host, port string) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port %q: %v", addr, err)
	}
	return host, port
}

func TestHandshake_Success(t *testing.T) {
	addr := mockServer(t, func(serverConn net.Conn) {
		tr := NewReadWriter(serverConn)

		// Read initial handshake.
		minVer, _ := tr.ReadInt32()
		maxVer, _ := tr.ReadInt32()
		db, _ := tr.ReadString()
		origURL, _ := tr.ReadString()
		user, _ := tr.ReadString()
		pwHash, _ := tr.ReadBytes()
		filePwHash, _ := tr.ReadBytes()
		nProps, _ := tr.ReadInt32()

		if minVer != TCPProtocolVersion21 {
			t.Errorf("minVer = %d, want %d", minVer, TCPProtocolVersion21)
		}
		if maxVer != TCPProtocolVersion21 {
			t.Errorf("maxVer = %d, want %d", maxVer, TCPProtocolVersion21)
		}
		if db == nil || *db != "testdb" {
			t.Errorf("db = %v, want \"testdb\"", db)
		}
		if origURL == nil || !strings.Contains(*origURL, "testdb") {
			t.Errorf("origURL = %v, want containing \"testdb\"", origURL)
		}
		if user == nil || *user != "SA" {
			t.Errorf("user = %v, want \"SA\"", user)
		}
		if len(pwHash) != 32 {
			t.Errorf("pwHash length = %d, want 32", len(pwHash))
		}
		if filePwHash != nil {
			t.Errorf("filePwHash = %v, want nil", filePwHash)
		}
		if nProps != 0 {
			t.Errorf("nProps = %d, want 0", nProps)
		}

		// Respond with OK + version 21.
		tr.WriteInt32(StatusOK)
		tr.WriteInt32(TCPProtocolVersion21)
		tr.Flush()

		// Read SESSION_SET_ID.
		op, _ := tr.ReadInt32()
		if op != SessionSetID {
			t.Errorf("op = %d, want %d (SessionSetID)", op, SessionSetID)
		}
		sid, _ := tr.ReadString()
		if sid == nil || len(*sid) != 64 {
			t.Errorf("sessionID length = %d, want 64", len(*sid))
		}
		_, _ = tr.ReadString() // timezone

		// Respond with OK + autoCommit=true.
		tr.WriteInt32(StatusOK)
		tr.WriteBool(true)
		tr.Flush()
	})

	host, port := parseAddr(t, addr)
	cfg := &Config{
		Host:        host,
		Port:        port,
		Database:    "testdb",
		User:        "sa",
		Password:    "",
		OriginalURL: "h2://localhost:9092/testdb",
	}

	sess, err := Handshake(cfg)
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	defer sess.Close()

	if sess.version != TCPProtocolVersion21 {
		t.Errorf("version = %d, want %d", sess.version, TCPProtocolVersion21)
	}
	if !sess.autoCommit {
		t.Errorf("autoCommit = %v, want true", sess.autoCommit)
	}
	if len(sess.id) != 64 {
		t.Errorf("session id length = %d, want 64", len(sess.id))
	}
}

func TestHandshake_VersionMismatch(t *testing.T) {
	addr := mockServer(t, func(serverConn net.Conn) {
		tr := NewReadWriter(serverConn)
		// Read and discard initial handshake.
		tr.ReadInt32()
		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()
		tr.ReadString()
		tr.ReadBytes()
		tr.ReadBytes()
		tr.ReadInt32()

		// Respond with OK but unsupported version 20.
		tr.WriteInt32(StatusOK)
		tr.WriteInt32(20)
		tr.Flush()
	})

	host, port := parseAddr(t, addr)
	cfg := &Config{
		Host:     host,
		Port:     port,
		Database: "testdb",
		User:     "SA",
		Password: "",
	}

	_, err := Handshake(cfg)
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
	if !errors.Is(err, ErrUnsupportedServerVersion) {
		t.Fatalf("expected ErrUnsupportedServerVersion, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "protocol 21") {
		t.Fatalf("error = %q, want message containing 'protocol 21'", err.Error())
	}
}

func TestHandshake_AuthError(t *testing.T) {
	addr := mockServer(t, func(serverConn net.Conn) {
		tr := NewReadWriter(serverConn)
		// Read and discard initial handshake.
		tr.ReadInt32()
		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()
		tr.ReadString()
		tr.ReadBytes()
		tr.ReadBytes()
		tr.ReadInt32()

		// Respond with STATUS_ERROR.
		tr.WriteInt32(StatusError)
		tr.WriteString("28000")
		tr.WriteString("Wrong user name or password")
		tr.WriteNullString()
		tr.WriteInt32(28000)
		tr.WriteString("stack trace")
		tr.Flush()
	})

	host, port := parseAddr(t, addr)
	cfg := &Config{
		Host:     host,
		Port:     port,
		Database: "testdb",
		User:     "BADUSER",
		Password: "wrong",
	}

	_, err := Handshake(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	h2err, ok := err.(*H2Error)
	if !ok {
		t.Fatalf("expected *H2Error, got %T: %v", err, err)
	}
	if h2err.SQLState != "28000" {
		t.Errorf("SQLState = %q, want \"28000\"", h2err.SQLState)
	}
	if !strings.Contains(h2err.Message, "Wrong user name or password") {
		t.Errorf("Message = %q, want containing \"Wrong user name or password\"", h2err.Message)
	}
	if h2err.Code != 28000 {
		t.Errorf("Code = %d, want 28000", h2err.Code)
	}
}

func TestHandshake_ErrorAfterSessionSetID(t *testing.T) {
	addr := mockServer(t, func(serverConn net.Conn) {
		tr := NewReadWriter(serverConn)
		// Read initial handshake.
		tr.ReadInt32()
		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()
		tr.ReadString()
		tr.ReadBytes()
		tr.ReadBytes()
		tr.ReadInt32()

		tr.WriteInt32(StatusOK)
		tr.WriteInt32(TCPProtocolVersion21)
		tr.Flush()

		// Read SESSION_SET_ID.
		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()

		// Respond with STATUS_ERROR.
		tr.WriteInt32(StatusError)
		tr.WriteString("90067")
		tr.WriteString("Connection is broken")
		tr.WriteNullString()
		tr.WriteInt32(90067)
		tr.WriteString(":java.lang.Throwable")
		tr.Flush()
	})

	host, port := parseAddr(t, addr)
	cfg := &Config{
		Host:     host,
		Port:     port,
		Database: "testdb",
		User:     "SA",
		Password: "",
	}

	_, err := Handshake(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	h2err, ok := err.(*H2Error)
	if !ok {
		t.Fatalf("expected *H2Error, got %T: %v", err, err)
	}
	if h2err.SQLState != "90067" {
		t.Errorf("SQLState = %q, want \"90067\"", h2err.SQLState)
	}
}

func TestHandshake_CorrectUserNameUppercasingOnWire(t *testing.T) {
	addr := mockServer(t, func(serverConn net.Conn) {
		tr := NewReadWriter(serverConn)
		tr.ReadInt32()
		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()
		user, _ := tr.ReadString()
		tr.ReadBytes()
		tr.ReadBytes()
		tr.ReadInt32()

		if user == nil || *user != "ROOT" {
			t.Errorf("user on wire = %v, want \"ROOT\"", user)
		}

		tr.WriteInt32(StatusOK)
		tr.WriteInt32(TCPProtocolVersion21)
		tr.Flush()

		tr.ReadInt32()
		tr.ReadString()
		tr.ReadString()
		tr.WriteInt32(StatusOK)
		tr.WriteBool(false)
		tr.Flush()
	})

	host, port := parseAddr(t, addr)
	cfg := &Config{
		Host:     host,
		Port:     port,
		Database: "testdb",
		User:     "root", // lower case in config
		Password: "secret",
	}

	sess, err := Handshake(cfg)
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	defer sess.Close()
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID: %v", err)
	}
	if len(id1) != 64 {
		t.Errorf("length = %d, want 64", len(id1))
	}

	id2, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID: %v", err)
	}
	if id1 == id2 {
		t.Error("two session IDs must not be identical")
	}

	// Must be hex only.
	for i, c := range id1 {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("session ID character %d = %q, not hex", i, c)
		}
	}
}

func TestSendCredentials_WireFormat(t *testing.T) {
	var buf bytes.Buffer
	wc := &writeCloseBuffer{&buf}
	tr := NewWriter(wc)

	cfg := &Config{
		Database:    "mydb",
		OriginalURL: "jdbc:h2:tcp://h2host:9092/mydb",
		User:        "admin",
		Password:    "secret",
	}

	if err := sendCredentials(tr, cfg); err != nil {
		t.Fatalf("sendCredentials: %v", err)
	}
	if err := tr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Re-read through a fresh Tr to verify structure.
	r := NewReader(&buf)

	minVer, _ := r.ReadInt32()
	if minVer != TCPProtocolVersion21 {
		t.Errorf("minVer = %d, want %d", minVer, TCPProtocolVersion21)
	}
	maxVer, _ := r.ReadInt32()
	if maxVer != TCPProtocolVersion21 {
		t.Errorf("maxVer = %d, want %d", maxVer, TCPProtocolVersion21)
	}
	db, _ := r.ReadString()
	if db == nil || *db != "mydb" {
		t.Errorf("db = %v, want \"mydb\"", db)
	}
	origURL, _ := r.ReadString()
	if origURL == nil || *origURL != cfg.OriginalURL {
		t.Errorf("origURL = %v, want %q", origURL, cfg.OriginalURL)
	}
	user, _ := r.ReadString()
	if user == nil || *user != "ADMIN" {
		t.Errorf("user = %v, want \"ADMIN\"", user)
	}
	pwHash, _ := r.ReadBytes()
	if len(pwHash) != 32 {
		t.Errorf("pwHash length = %d, want 32", len(pwHash))
	}
	filePwHash, _ := r.ReadBytes()
	if filePwHash != nil {
		t.Errorf("filePwHash = %v, want nil", filePwHash)
	}
	nProps, _ := r.ReadInt32()
	if nProps != 0 {
		t.Errorf("nProps = %d, want 0", nProps)
	}
}
