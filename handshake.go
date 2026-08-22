package h2go

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
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
	return HandshakeContext(context.Background(), cfg)
}

// HandshakeContext is the context-aware form of Handshake. It applies any
// caller deadline to the underlying TCP socket and aborts the connection on
// cancellation so the caller never gets a half-open session.
func HandshakeContext(ctx context.Context, cfg *Config) (sess *Session, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return nil, fmt.Errorf("h2go: HandshakeContext: config is nil")
	}
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	logConfig(cfg, slog.LevelDebug, "handshake dial starting")
	dialer := net.Dialer{}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		logConfig(cfg, slog.LevelError, "handshake dial failed", slog.Any("error", err))
		return nil, fmt.Errorf("h2go: dial %s: %w", addr, err)
	}

	tr := NewReadWriter(conn)
	cleanup := beginOperationContext(ctx, tr, nil)
	defer cleanup()

	handshakeAutoCommit := false
	defer func() {
		if err != nil {
			logConfig(cfg, slog.LevelError, "handshake failed", slog.Any("error", err))
			_ = tr.Abort()
			if ctx.Err() != nil {
				err = ctx.Err()
			} else if deadline, ok := ctx.Deadline(); ok {
				// Same deadline-race handling as Session.finalizeContext: a
				// timeout-kind transport error can surface just before the
				// context timer marks the context done.
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() && time.Now().After(deadline) {
					err = context.DeadlineExceeded
				}
			}
			return
		}
		logConfig(cfg, slog.LevelDebug, "handshake completed", slog.Bool("autocommit", handshakeAutoCommit))
	}()

	// Step 1: send credentials and requested protocol version.
	if e := sendCredentials(tr, cfg); e != nil {
		err = fmt.Errorf("h2go: send credentials: %w", e)
		return nil, err
	}
	if e := tr.Flush(); e != nil {
		err = fmt.Errorf("h2go: flush credentials: %w", e)
		return nil, err
	}

	// Step 2: read status and negotiated version.
	status, e := tr.ReadInt32()
	if e != nil {
		err = fmt.Errorf("h2go: read status: %w", e)
		return nil, err
	}

	switch status {
	case StatusError:
		err = readH2Error(tr)
		return nil, err
	case StatusOK:
		// continue
	default:
		err = fmt.Errorf("h2go: unexpected status %d", status)
		return nil, err
	}

	version, e := tr.ReadInt32()
	if e != nil {
		err = fmt.Errorf("h2go: read version: %w", e)
		return nil, err
	}
	if version != TCPProtocolVersion21 {
		err = fmt.Errorf(
			"%w; require protocol 21 / H2 2.4.240+ (got %d)",
			ErrUnsupportedServerVersion, version)
		return nil, err
	}

	// Step 3: send SESSION_SET_ID.
	sessionID, e := generateSessionID()
	if e != nil {
		err = fmt.Errorf("h2go: generate session id: %w", e)
		return nil, err
	}

	if e := tr.WriteInt32(SessionSetID); e != nil {
		err = fmt.Errorf("h2go: write session set id op: %w", e)
		return nil, err
	}
	if e := tr.WriteString(sessionID); e != nil {
		err = fmt.Errorf("h2go: write session id: %w", e)
		return nil, err
	}
	if e := tr.WriteString(localTimeZoneID(cfg)); e != nil {
		err = fmt.Errorf("h2go: write timezone id: %w", e)
		return nil, err
	}
	if e := tr.Flush(); e != nil {
		err = fmt.Errorf("h2go: flush session set id: %w", e)
		return nil, err
	}

	// Step 4: read status and autocommit state.
	status, e = tr.ReadInt32()
	if e != nil {
		err = fmt.Errorf("h2go: read session status: %w", e)
		return nil, err
	}
	switch status {
	case StatusError:
		err = readH2Error(tr)
		return nil, err
	case StatusOK:
		// continue
	default:
		err = fmt.Errorf("h2go: unexpected status %d after session set id", status)
		return nil, err
	}

	autoCommit, e := tr.ReadBool()
	if e != nil {
		err = fmt.Errorf("h2go: read autocommit: %w", e)
		return nil, err
	}
	handshakeAutoCommit = autoCommit

	return &Session{
		tr:         tr,
		id:         sessionID,
		version:    version,
		autoCommit: autoCommit,
		cfg:        cloneConfig(cfg),
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

// localTimeZoneID returns the timezone identifier sent to the server during
// the SESSION_SET_ID handshake step (required for protocol 20+) so it renders
// TIMESTAMP WITH TIME ZONE values in local time.
//
// The candidate (TZ environment variable, else the system zone name) is
// validated with time.LoadLocation: an unparseable value would otherwise be
// sent verbatim and leave the server unable to resolve it, so it falls back to
// "UTC" with a debug log.
func localTimeZoneID(cfg *Config) string {
	candidate := ""
	if tz := os.Getenv("TZ"); tz != "" {
		candidate = tz
	} else if name := time.Local.String(); name != "Local" {
		candidate = name
	}
	if candidate == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(candidate); err != nil {
		logConfig(cfg, slog.LevelDebug,
			"unparseable local timezone, falling back to UTC",
			slog.String("candidate", candidate), slog.Any("error", err))
		return "UTC"
	}
	return candidate
}
