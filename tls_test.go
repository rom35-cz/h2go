// tls_test.go — unit tests for TLS DSN parsing and the TLS transport wrap.
//
// The transport test spins up an in-process crypto/tls listener with a
// freshly generated self-signed certificate; live verification against a
// real -tcpSSL H2 server runs in TestIntegration_TLSTransport.

package h2go

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseDSNTLSSchemes(t *testing.T) {
	tests := []struct {
		dsn     string
		wantTLS bool
		wantDB  string
	}{
		{"jdbc:h2:ssl://localhost:9093/h2-go", true, "h2-go"},
		{"jdbc:h2:ssl://localhost/h2-go;USER=u;PASSWORD=p", true, "h2-go"},
		{"jdbc:h2:tcp://localhost:9092/h2-go", false, "h2-go"},
		{"h2+ssl://user:secret@localhost:9093/dbname", true, "dbname"},
		{"h2://u:p@localhost/db", false, "db"},
	}
	for _, tc := range tests {
		cfg, err := ParseDSN(tc.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tc.dsn, err)
			continue
		}
		if cfg.TLS != tc.wantTLS {
			t.Errorf("ParseDSN(%q).TLS = %v, want %v", tc.dsn, cfg.TLS, tc.wantTLS)
		}
		if cfg.Database != tc.wantDB {
			t.Errorf("ParseDSN(%q).Database = %q, want %q", tc.dsn, cfg.Database, tc.wantDB)
		}
	}

	// Non-TCP modes stay rejected.
	for _, bad := range []string{"jdbc:h2:mem:test", "jdbc:h2:file:/tmp/x"} {
		if _, err := ParseDSN(bad); err == nil {
			t.Errorf("ParseDSN(%q): expected error", bad)
		}
	}
	// Unknown native schemes still fail with the updated hint text.
	if _, err := ParseDSN("h2+quic://x/db"); err == nil || !strings.Contains(err.Error(), "h2+ssl://") {
		t.Errorf("ParseDSN(h2+quic) error = %v, want scheme hint mentioning h2+ssl://", err)
	}
}

// selfSignedCert generates a quick self-signed certificate for host.
func selfSignedCert(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key gen: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("cert parse: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// TestWrapTLSVerifiedAndSkipped runs a real TLS handshake against an
// in-process listener: once failing verification (self-signed, no trust),
// once succeeding with the cert pinned as root CA.
func TestWrapTLSVerifiedAndSkipped(t *testing.T) {
	const host = "localhost"
	serverCert, pool := selfSignedCert(t, host)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.(*tls.Conn).HandshakeContext(context.Background())
		}
	}()

	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	cfg := &Config{Host: "127.0.0.1", Port: port}

	// 1. Untrusting client: handshake must fail with a verification error.
	raw, err := net.DialTimeout("tcp", addrForLog(cfg), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := wrapTLS(ctx, raw, cfg); err == nil {
		t.Error("expected verification failure against self-signed server")
	} else if !isTLSVerifyError(err) {
		t.Errorf("unexpected error class: %v", err)
	}

	// 2. Trusting client via pinned root CA and TLSServerName override
	// (dialing by IP against a cert issued for "localhost"): handshake must
	// succeed.
	cfg.TLSRootCAs = pool
	cfg.TLSServerName = host
	raw2, err := net.DialTimeout("tcp", addrForLog(cfg), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	conn, err := wrapTLS(ctx2, raw2, cfg)
	if err != nil {
		t.Fatalf("wrapTLS with pinned CA: %v", err)
	}
	_ = conn.Close()

	// 3. Skip-verify client must also succeed (and not need the pool).
	cfg3 := &Config{Host: "127.0.0.1", Port: port, TLSInsecureSkipVerify: true}
	raw3, err := net.DialTimeout("tcp", addrForLog(cfg3), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	conn3, err := wrapTLS(ctx3, raw3, cfg3)
	if err != nil {
		t.Fatalf("wrapTLS skip-verify: %v", err)
	}
	_ = conn3.Close()
}

func isTLSVerifyError(err error) bool {
	var unknownAuthority *x509.UnknownAuthorityError
	var hostnameErr *x509.HostnameError
	return errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameErr) ||
		strings.Contains(err.Error(), "certificate")
}
