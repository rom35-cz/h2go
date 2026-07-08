package h2go

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Config holds parsed H2 connection parameters.
type Config struct {
	// Host is the server hostname or IP address (required).
	Host string
	// Port is the TCP port as a string. Defaults to "9092" if not specified.
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
	Params map[string]string
	// OriginalURL is the exact DSN string supplied to ParseDSN,
	// preserved for the handshake.
	OriginalURL string
}

// ParseDSN parses an H2 DSN string into a Config.
//
// Supported formats:
//
//	  JDBC-style:
//		   jdbc:h2:tcp://host:port/database
//		   jdbc:h2:tcp://host:port/database;PARAM1=val1;PARAM2=val2
//
//	  Native Go DSN:
//		   h2://user:password@host:port/database?k=v
//		   h2+tcp://user:password@host:port/database?k=v
//
// The default port is 9092 when omitted.
// For JDBC-style DSNs, semicolon-separated parameters are parsed and
// available in Params. USER and PASSWORD parameters are extracted into
// Config.User and Config.Password.
// For native DSNs, query parameters (?k=v) are parsed with standard
// percent-decoding and placed in Params. Userinfo (user:password@) is
// extracted into Config.User and Config.Password.
//
// Unsupported H2 connection modes (mem:, file:, ssl:) return an error.
func ParseDSN(input string) (*Config, error) {
	if input == "" {
		return nil, errors.New("empty DSN")
	}

	switch {
	case strings.HasPrefix(input, "jdbc:h2:"):
		return parseJDBC(input)
	case strings.HasPrefix(input, "h2://") || strings.HasPrefix(input, "h2+tcp://"):
		return parseNative(input)
	default:
		return nil, fmt.Errorf("unsupported DSN scheme: %q (supported: jdbc:h2:, h2://, h2+tcp://)", input)
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

func parseJDBC(input string) (*Config, error) {
	const prefix = "jdbc:h2:"
	if !strings.HasPrefix(input, prefix) {
		return nil, fmt.Errorf("unsupported DSN scheme: %q", input)
	}

	cfg := &Config{
		OriginalURL: input,
		Port:        "9092",
		Params:      make(map[string]string),
	}

	inner := strings.TrimPrefix(input, prefix)

	// Reject non-TCP H2 connection modes.
	if strings.HasPrefix(inner, "mem:") || strings.HasPrefix(inner, "file:") || strings.HasPrefix(inner, "ssl:") {
		return nil, fmt.Errorf("unsupported H2 connection mode: %q (only tcp is supported)", inner)
	}
	if !strings.HasPrefix(inner, "tcp:") {
		return nil, fmt.Errorf("unsupported H2 protocol: expected tcp://, got %q", inner)
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
			cfg.Params[p] = ""
			continue
		}
		key := p[:idx]
		val := p[idx+1:]
		cfg.Params[key] = val
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
		Port:        "9092",
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

	if u.RawQuery != "" {
		q := u.Query()
		for k, vals := range q {
			if len(vals) > 0 {
				cfg.Params[k] = vals[0]
			}
		}
	}

	return validate(cfg)
}

func validate(cfg *Config) (*Config, error) {
	if cfg.Host == "" {
		return nil, errors.New("missing host in DSN")
	}
	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", cfg.Port, err)
	}
	return cfg, nil
}
