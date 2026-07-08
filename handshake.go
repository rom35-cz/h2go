package h2go

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// Handshake establishes a TCP connection to the H2 server and completes the
// authentication handshake using protocol 21.
//
// The handshake follows the sequence defined by H2 TcpServerThread and
// SessionRemote:
//
//  1. Client sends protocol version (min=max=21), database name, original
//     URL, user name (uppercased), user password hash, file password hash
//     (nil for TCP), and connection property count (0).
//  2. Server replies with status (STATUS_OK) and negotiated version.
//  3. Client sends SESSION_SET_ID with a random session id and local
//     timezone id (required for protocol 20+).
//  4. Server replies with status (STATUS_OK) and autocommit boolean.
//
// If the server returns a version other than 21, or any STATUS_ERROR,
// the connection is closed and an error is returned.
func Handshake(cfg *Config) (*Session, error) {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("h2go: dial %s: %w", addr, err)
	}

	tr := NewReadWriter(conn)

	// Step 1: send credentials and requested protocol version.
	if err := sendCredentials(tr, cfg); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: send credentials: %w", err)
	}
	if err := tr.Flush(); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: flush credentials: %w", err)
	}

	// Step 2: read status and negotiated version.
	status, err := tr.ReadInt32()
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: read status: %w", err)
	}

	switch status {
	case StatusError:
		err = readH2Error(tr)
		_ = tr.Close()
		return nil, err
	case StatusOK:
		// continue
	default:
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: unexpected status %d", status)
	}

	version, err := tr.ReadInt32()
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: read version: %w", err)
	}
	if version != TCPProtocolVersion21 {
		_ = tr.Close()
		return nil, fmt.Errorf(
			"unsupported H2 server version; require protocol 21 / H2 2.4.240+ (got %d)",
			version)
	}

	// Step 3: send SESSION_SET_ID.
	sessionID, err := generateSessionID()
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: generate session id: %w", err)
	}

	if err := tr.WriteInt32(SessionSetID); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: write session set id op: %w", err)
	}
	if err := tr.WriteString(sessionID); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: write session id: %w", err)
	}
	if err := tr.WriteString(localTimeZoneID()); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: write timezone id: %w", err)
	}
	if err := tr.Flush(); err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: flush session set id: %w", err)
	}

	// Step 4: read status and autocommit state.
	status, err = tr.ReadInt32()
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: read session status: %w", err)
	}
	switch status {
	case StatusError:
		err = readH2Error(tr)
		_ = tr.Close()
		return nil, err
	case StatusOK:
		// continue
	default:
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: unexpected status %d after session set id", status)
	}

	autoCommit, err := tr.ReadBool()
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("h2go: read autocommit: %w", err)
	}

	return &Session{
		tr:         tr,
		id:         sessionID,
		version:    version,
		autoCommit: autoCommit,
	}, nil
}

// sendCredentials writes the initial handshake payload to tr.
func sendCredentials(tr *Tr, cfg *Config) error {
	if err := tr.WriteInt32(TCPProtocolVersionMinSupported); err != nil {
		return err
	}
	if err := tr.WriteInt32(TCPProtocolVersionMaxSupported); err != nil {
		return err
	}
	if err := tr.WriteString(cfg.Database); err != nil {
		return err
	}
	if err := tr.WriteString(cfg.OriginalURL); err != nil {
		return err
	}
	// H2 uppercases the user name before sending (ConnectionInfo.setUserName).
	if err := tr.WriteString(strings.ToUpper(cfg.User)); err != nil {
		return err
	}
	if err := tr.WriteBytes(userPasswordHash(cfg.User, cfg.Password)); err != nil {
		return err
	}
	// File password: nil for TCP mode (not a file-encrypted database).
	if err := tr.WriteBytes(nil); err != nil {
		return err
	}
	// Connection properties: none for now.
	if err := tr.WriteInt32(0); err != nil {
		return err
	}
	return nil
}

// generateSessionID creates a 64-character hex string from 32 random bytes,
// matching SessionRemote's session id format (MathUtils.secureRandomBytes(32)).
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
