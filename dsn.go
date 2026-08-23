package h2go

import (
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Config holds parsed H2 connection parameters.
type Config struct {
	// Host is the server hostname or IP address (required).
	Host string
	// Port is the TCP port as a string. Defaults to DefaultTCPPortStr if not specified.
	Port string
	// Database is the H2 database name. The leading "/" from the URL path
	// is stripped (e.g. "/h2-go" becomes "h2-go").
	Database string
	// User is the username extracted from JDBC ;USER=..., native URL userinfo,
	// or an explicit override.
	User string
	// Password is the password extracted from JDBC ;PASSWORD=..., native URL
	// userinfo, or an explicit override.
	Password string
	// Params holds JDBC semicolon-separated or native query parameters not
	// otherwise consumed.
	//
	// Parameter policy (mirrors H2's own client semantics):
	//
	//   - USER / PASSWORD are extracted into Config.User / Config.Password.
	//   - Forwarded settings (IFEXISTS, ACCESS_MODE_DATA, INIT, MODE,
	//     LOCK_TIMEOUT, FORBID_CREATION) are sent to the server in the
	//     handshake property map; the server enforces them when opening the
	//     database.
	//   - Recognized client-side settings (e.g. AUTO_SERVER, TRACE_LEVEL_*)
	//     are accepted but have no effect on this pure-TCP driver.
	//   - Anything else is REJECTED with an error at parse time unless the
	//     DSN also carries IGNORE_UNKNOWN_SETTINGS=TRUE — mirroring H2 JDBC.
	//
	// Keys are matched case-insensitively; original spellings are preserved
	// in this map.
	Params map[string]string
	// OriginalURL is the exact DSN string supplied to ParseDSN,
	// preserved for the handshake.
	OriginalURL string

	// Logger enables optional driver diagnostics when non-nil.
	// ParseDSN never populates Logger; callers must set it explicitly via
	// Config / connector APIs (DSN strings cannot carry logger objects).
	Logger *slog.Logger

	// MaxRows bounds the number of rows the server will materialize for a
	// single result set. It is forwarded as the protocol maxRows parameter of
	// COMMAND_EXECUTE_QUERY. Zero (the default) means no limit, matching the
	// H2 server semantics. Negative values are treated as zero.
	MaxRows int64

	// FetchSize controls how many rows the driver requests per
	// RESULT_FETCH_ROWS batch while streaming a result set. Zero (the default)
	// uses defaultFetchSize (100). Negative values are treated as zero.
	FetchSize int

	// GeneratedKeysMode controls the generated keys request mode sent with
	// COMMAND_EXECUTE_UPDATE. When zero (default), the driver uses
	// GeneratedKeysAuto, mirroring JDBC Statement.RETURN_GENERATED_KEYS.
	// Set to one of the GeneratedKeys* constants to override:
	//   - GeneratedKeysAuto (1): auto-detect generated key column.
	//   - GeneratedKeysNone (0): no generated keys requested.
	//   - GeneratedKeysColumnNumbers (2): use GeneratedKeysColumns.
	//   - GeneratedKeysColumnNames (3): use GeneratedKeysColumnNames.
	//
	// Scope: CONNECTION-LEVEL. The mode applies to every update statement
	// executed on connections created from this Config — unlike JDBC, where
	// the request is made per statement. To mix modes, create separate
	// *sql.DB handles from separate Configs. Per-statement overrides are
	// future work.
	//
	// Note: GeneratedKeysNone == 0, which is the zero value; the driver
	// treats 0 as "default" (auto) unless GeneratedKeysModeSet is true.
	GeneratedKeysMode int

	// GeneratedKeysModeSet marks that GeneratedKeysMode was explicitly set
	// by the caller. When false (default), the driver uses GeneratedKeysAuto
	// regardless of the GeneratedKeysMode field value.
	//
	// This escape hatch matters because GeneratedKeysNone == 0: without it,
	// setting GeneratedKeysMode = GeneratedKeysNone has no effect and keys
	// keep being requested. The same connection-level scope as
	// GeneratedKeysMode applies.
	GeneratedKeysModeSet bool

	// GeneratedKeysColumns holds column indices (1-based) for generated keys
	// when GeneratedKeysMode is GeneratedKeysColumnNumbers. Same
	// connection-level scope as GeneratedKeysMode.
	GeneratedKeysColumns []int

	// GeneratedKeysColumnNames holds column names for generated keys when
	// GeneratedKeysMode is GeneratedKeysColumnNames. Same connection-level
	// scope as GeneratedKeysMode.
	GeneratedKeysColumnNames []string

	// TLS enables TLS for the transport. It is set automatically for DSNs
	// using the ssl:// scheme (jdbc:h2:ssl://host:port/db, h2+ssl://...),
	// mirroring H2's own client behavior, and can also be enabled
	// programmatically for tcp:// DSNs that target a server started with
	// the -tcpSSL flag.
	TLS bool

	// TLSServerName optionally overrides the hostname used for server
	// certificate verification (and SNI). Empty means cfg.Host.
	TLSServerName string

	// TLSInsecureSkipVerify disables server certificate chain and hostname
	// verification, like crypto/tls InsecureSkipVerify. Intended for local
	// development servers with self-signed certificates; never disable in
	// production.
	TLSInsecureSkipVerify bool

	// TLSRootCAs optionally supplies the certificate pool used to verify
	// the server certificate. Nil uses the system roots. Useful for private
	// CAs and self-signed test servers.
	TLSRootCAs *x509.CertPool
}

// ParseDSN parses an H2 DSN string into a Config.
//
// JDBC-style format:
//
//	jdbc:h2:tcp://host:port/database
//	jdbc:h2:tcp://host:port/database;PARAM1=val1;PARAM2=val2
//
// Native Go DSN format:
//
//	h2://user:password@host:port/database?k=v
//	h2+tcp://user:password@host:port/database?k=v
//
// TLS DSNs use the same shapes with the ssl scheme, matching H2's own
// client:
//
//	jdbc:h2:ssl://host:port/database;PARAM1=val1;PARAM2=val2
//	h2+ssl://user:password@host:port/database?k=v
//
// The default port is 9092 when omitted.
// For JDBC-style DSNs, semicolon-separated parameters are parsed and
// available in Params. USER and PASSWORD parameters are extracted into
// Config.User and Config.Password.
// For native DSNs, query parameters (?k=v) are parsed with standard
// percent-decoding and placed in Params. Userinfo (user:password@) is
// extracted into Config.User and Config.Password.
//
// Unsupported H2 connection modes (mem:, file:) return an error; the ssl://
// scheme selects TLS and is mapped to Config.TLS.
func ParseDSN(input string) (*Config, error) {
	if input == "" {
		return nil, errors.New("empty DSN")
	}

	switch {
	case strings.HasPrefix(input, "jdbc:h2:"):
		return parseJDBC(input)
	case strings.HasPrefix(input, "h2://") || strings.HasPrefix(input, "h2+tcp://") || strings.HasPrefix(input, "h2+ssl://"):
		return parseNative(input)
	default:
		return nil, fmt.Errorf("unsupported DSN scheme: %q (supported: jdbc:h2:, h2://, h2+tcp://, h2+ssl://)", input)
	}
}

// MergeCredentials overlays user and password into cfg only when the
// corresponding field in cfg is empty. Parsed credentials (from JDBC
// parameters or native userinfo) take precedence over externally supplied
// values.
//
// Precedence (highest to lowest):
//  1. Credentials present in the parsed DSN itself (JDBC ;USER=/;PASSWORD=
//     or native userinfo).
//  2. Values supplied to MergeCredentials (e.g. from environment variables).
//  3. Empty / omitted.
func MergeCredentials(cfg *Config, user, password string) {
	if cfg.User == "" {
		cfg.User = user
	}
	if cfg.Password == "" {
		cfg.Password = password
	}
}

// cloneConfig makes a shallow copy of cfg for long-lived connection/session
// state. Params remains shared (matching NewConnector's behavior) because the
// driver treats the map as immutable after parsing.
func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	return &cloned
}

func parseJDBC(input string) (*Config, error) {
	// ParseDSN guarantees the "jdbc:h2:" prefix before calling here.
	const prefix = "jdbc:h2:"

	cfg := &Config{
		OriginalURL: input,
		Port:        DefaultTCPPortStr,
		Params:      make(map[string]string),
	}

	inner := strings.TrimPrefix(input, prefix)

	// Reject non-TCP H2 connection modes; ssl:// selects TLS over TCP.
	if strings.HasPrefix(inner, "mem:") || strings.HasPrefix(inner, "file:") {
		return nil, fmt.Errorf("unsupported H2 connection mode: %q (only tcp and ssl are supported)", inner)
	}
	if strings.HasPrefix(inner, "ssl:") {
		cfg.TLS = true
	} else if !strings.HasPrefix(inner, "tcp:") {
		return nil, fmt.Errorf("unsupported H2 protocol: expected tcp:// or ssl://, got %q", inner)
	}

	u, err := url.Parse(inner)
	if err != nil {
		return nil, fmt.Errorf("invalid URL after jdbc:h2: prefix: %w", err)
	}

	cfg.Host = u.Hostname()
	if u.Port() != "" {
		cfg.Port = u.Port()
	}

	// Split path on ';' to separate database name from JDBC parameters.
	rawPath := u.Path
	parts := strings.Split(rawPath, ";")

	// First segment is the database name. Strip a leading "/" as required
	// by the database-name rule (PRD §7.2).
	db := strings.TrimPrefix(parts[0], "/")
	cfg.Database = db

	// Remaining segments are semicolon-separated key=value parameters.
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		idx := strings.Index(p, "=")
		if idx < 0 {
			if err := setParam(cfg.Params, p, ""); err != nil {
				return nil, err
			}
			continue
		}
		key := p[:idx]
		val := p[idx+1:]
		if err := setParam(cfg.Params, key, val); err != nil {
			return nil, err
		}
		if strings.EqualFold(key, "USER") {
			cfg.User = val
		} else if strings.EqualFold(key, "PASSWORD") {
			cfg.Password = val
		}
	}

	return validate(cfg)
}

func parseNative(input string) (*Config, error) {
	cfg := &Config{
		OriginalURL: input,
		Port:        DefaultTCPPortStr,
		Params:      make(map[string]string),
	}

	u, err := url.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("invalid native DSN: %w", err)
	}

	cfg.Host = u.Hostname()
	if u.Port() != "" {
		cfg.Port = u.Port()
	}

	db := strings.TrimPrefix(u.Path, "/")
	cfg.Database = db

	if u.User != nil {
		cfg.User = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			cfg.Password = pw
		}
	}

	if strings.HasPrefix(input, "h2+ssl://") {
		cfg.TLS = true
	}

	if u.RawQuery != "" {
		q := u.Query()
		for k, vals := range q {
			if len(vals) > 0 {
				if err := setParam(cfg.Params, k, vals[0]); err != nil {
					return nil, err
				}
			}
		}
	}

	return validate(cfg)
}

func validate(cfg *Config) (*Config, error) {
	if cfg.Host == "" {
		return nil, errors.New("missing host in DSN")
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", cfg.Port, err)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port %q: must be in range 1-65535", cfg.Port)
	}
	if err := validateParams(cfg.Params); err != nil {
		return nil, err
	}
	return cfg, nil
}

// forwardSettings are sent to the server in the handshake property map; the
// server-side ConnectionInfo enforces them while opening the database
// (reference: SessionRemote.initTransfer writes all connection properties;
// TcpServerThread feeds them into ConnectionInfo.setProperty).
var forwardSettings = map[string]bool{
	"ACCESS_MODE_DATA": true, // r | rw | rws: session read-only mode
	"FORBID_CREATION":  true, // forbid implicit database creation
	"IFEXISTS":         true, // fail unless the database already exists
	"INIT":             true, // SQL run right after connect (server-side)
	"LOCK_TIMEOUT":     true, // default lock wait in milliseconds
	"MODE":             true, // server-side compatibility mode
}

// localOnlySettings are recognized H2 connection settings that only matter to
// embedded/JDBC-client deployments. They are accepted (so URLs carrying them
// keep working) but have no effect on this pure-TCP driver.
var localOnlySettings = map[string]bool{
	"AUTO_RECONNECT":         true,
	"AUTO_SERVER":            true,
	"CIPHER":                 true,
	"DB_CLOSE_DELAY":         true,
	"DB_CLOSE_ON_EXIT":       true,
	"FILE_LOCK":              true,
	"JMX":                    true,
	"NETWORK_TIMEOUT":        true,
	"NON_KEYWORDS":           true,
	"OLD_INFORMATION_SCHEMA": true,
	"OPEN_NEW":               true,
	"PAGE_SIZE":              true,
	"RECOVER":                true,
	"STATEMENT_CACHE_SIZE":   true,
	"TRACE_LEVEL_FILE":       true,
	"TRACE_LEVEL_SYSTEM_OUT": true,
}

// validateParams enforces the unknown-setting policy: any parameter outside
// the known sets is rejected unless IGNORE_UNKNOWN_SETTINGS=TRUE, mirroring
// H2's ConnectionInfo behavior (UNSUPPORTED_SETTING vs IGNORE_UNKNOWN_SETTINGS).
// USER/PASSWORD never reach here — they are consumed earlier.
func validateParams(params map[string]string) error {
	ignoreUnknown := false
	var unknown []string
	for k := range params {
		switch strings.ToUpper(k) {
		case "IGNORE_UNKNOWN_SETTINGS":
			ignoreUnknown = isTruthy(params[k])
		case "USER", "PASSWORD":
			// consumed into Config before validate runs; tolerate remnants
		default:
			if !forwardSettings[strings.ToUpper(k)] && !localOnlySettings[strings.ToUpper(k)] {
				unknown = append(unknown, k)
			}
		}
	}
	if len(unknown) == 0 || ignoreUnknown {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unsupported DSN setting(s): %s (prefix the DSN with IGNORE_UNKNOWN_SETTINGS=TRUE to allow unknown settings, as with H2 JDBC)",
		strings.Join(unknown, ", "))
}

func isTruthy(v string) bool {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1", "TRUE", "YES", "ON":
		return true
	}
	return false
}

// setParam inserts a DSN setting case-insensitively: an exact-value repeat is
// accepted (original spelling wins); a conflicting value is a duplicate-
// property error, mirroring ConnectionInfo's DUPLICATE_PROPERTY handling.
func setParam(params map[string]string, key, value string) error {
	for k, v := range params {
		if strings.EqualFold(k, key) {
			if v != value {
				return fmt.Errorf("duplicate DSN setting %q with conflicting values %q and %q", key, v, value)
			}
			return nil
		}
	}
	params[key] = value
	return nil
}

// sessionPropertyMap builds the handshake property map from cfg.Params:
// every forwarded setting, upper-cased key, sorted for deterministic frames.
// Values pass through verbatim; the server validates them.
func sessionPropertyMap(params map[string]string) [][2]string {
	type kv = [2]string
	out := make([]kv, 0, len(params))
	for k, v := range params {
		up := strings.ToUpper(k)
		if up == "IGNORE_UNKNOWN_SETTINGS" {
			// Consumed by the client; not a server-side session property.
			continue
		}
		if forwardSettings[up] {
			out = append(out, kv{up, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
