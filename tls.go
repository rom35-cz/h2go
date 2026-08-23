// tls.go — TLS transport support.
//
// H2's native TCP server enables TLS with the -tcpSSL flag; its sockets come
// from the JVM default SSL factories configured through the standard
// javax.net.ssl.keyStore/keyStorePassword system properties (or the default
// ~/.h2.keystore). H2 clients select TLS via the ssl:// URL scheme
// (JdbcUtils.getConnection → NetUtils.createSocket(..., ssl=true)).
//
// The driver mirrors this: ssl:// DSNs set Config.TLS, and the transport is
// wrapped in crypto/tls after dialing and before any protocol bytes flow.
// The statement-cancel side channel dials the same port, so it wraps too.

package h2go

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
)

// wrapTLS upgrades an established TCP connection to TLS using cfg. The
// returned connection must be used for all further protocol I/O; on error
// the raw connection is closed.
//
// Verification follows crypto/tls defaults: system roots unless
// cfg.TLSRootCAs is set, hostname from TLSServerName or else cfg.Host, and
// verification skipped only when cfg.TLSInsecureSkipVerify is set.
func wrapTLS(ctx context.Context, raw net.Conn, cfg *Config) (net.Conn, error) {
	serverName := cfg.TLSServerName
	if serverName == "" {
		// Mirror Java's SSLSocket behavior: verification targets whatever
		// name/IP was dialed unless overridden.
		serverName = cfg.Host
	}
	tc := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		RootCAs:            cfg.TLSRootCAs,
		InsecureSkipVerify: cfg.TLSInsecureSkipVerify,
	}

	tconn := tls.Client(raw, tc)

	// HandshakeContext honors ctx cancellation directly; the caller's
	// deadline therefore bounds both TCP connect and the TLS handshake.
	if err := tconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("h2go: tls handshake with %s: %w", addrForLog(cfg), err)
	}
	return tconn, nil
}

func addrForLog(cfg *Config) string {
	return net.JoinHostPort(cfg.Host, cfg.Port)
}
