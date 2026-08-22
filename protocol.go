package h2go

// TCP protocol version and port constants.
const (
	// TCPProtocolVersion20 is the previous TCP protocol version.
	// Used for conditional wire format differences.
	TCPProtocolVersion20 = 20

	// TCPProtocolVersion21 is the native TCP protocol version used by this
	// driver. Only protocol 21 is supported.
	TCPProtocolVersion21 = 21

	// TCPProtocolVersionMinSupported is the minimum supported version for
	// protocol negotiation. Must equal TCPProtocolVersion21.
	TCPProtocolVersionMinSupported = TCPProtocolVersion21

	// TCPProtocolVersionMaxSupported is the maximum supported version for
	// protocol negotiation. Must equal TCPProtocolVersion21.
	TCPProtocolVersionMaxSupported = TCPProtocolVersion21

	// DefaultTCPPort is the default H2 TCP server port.
	DefaultTCPPort = 9092

	// DefaultTCPPortStr is the string form of DefaultTCPPort, used wherever
	// port values are stored as strings (e.g. Config.Port).
	DefaultTCPPortStr = "9092"
)

// Status codes returned by the H2 server after a request.
//
// Reference: org.h2.engine.SessionRemote
const (
	StatusOK             = 1 // Request succeeded.
	StatusError          = 0 // Request failed; an error follows.
	StatusClosed         = 2 // Connection is closed by the server.
	StatusOKStateChanged = 3 // Request succeeded and autocommit state changed.
)

// Operation codes sent by the client for each request.
//
// Reference: org.h2.engine.SessionRemote
const (
	SessionPrepare               = 0  // Prepare a statement.
	SessionClose                 = 1  // Close the session.
	CommandExecuteQuery          = 2  // Execute a query (returns rows).
	CommandExecuteUpdate         = 3  // Execute an update/insert/delete.
	CommandClose                 = 4  // Close a prepared statement.
	ResultFetchRows              = 5  // Fetch more rows from a result set.
	ResultReset                  = 6  // Reset a result set cursor.
	ResultClose                  = 7  // Close a result set.
	CommandCommit                = 8  // Commit or rollback the current transaction.
	ChangeID                     = 9  // Update the session identity.
	CommandGetMetaData           = 10 // Get metadata for a prepared statement.
	SessionSetID                 = 12 // Set the session ID.
	SessionCancelStatement       = 13 // Cancel a running statement.
	SessionCheckKey              = 14 // Validate the session key.
	SessionSetAutocommit         = 15 // Set autocommit mode.
	SessionHasPendingTransaction = 16 // Check for pending transaction.
	LobRead                      = 17 // Read a LOB chunk.
	SessionPrepareReadParams2    = 18 // Prepare and read parameter metadata.
	GetJdbcMeta                  = 19 // Request JDBC metadata.
	CommandExecuteBatchUpdate    = 20 // Execute a batch update.
)

// GeneratedKeysMode constants for generated-keys configuration.
// These match the enum ordinals in org.h2.engine.GeneratedKeysMode:
//   NONE = 0, AUTO = 1, COLUMN_NUMBERS = 2, COLUMN_NAMES = 3
//
// Reference: org.h2.engine.GeneratedKeysMode
const (
	GeneratedKeysNone          = 0 // Generated keys are not needed.
	GeneratedKeysAuto          = 1 // Generated keys are configured automatically.
	GeneratedKeysColumnNumbers = 2 // Use specified column indices for generated keys.
	GeneratedKeysColumnNames   = 3 // Use specified column names for generated keys.
)

// Value type codes sent on the wire. These match the internal type identifiers
// used by H2 Transfer (org.h2.value.Transfer).
//
// Reference: org.h2.value.Transfer
const (
	ValueTypeNull              = 0  // NULL
	ValueTypeBoolean           = 1  // BOOLEAN
	ValueTypeTinyint           = 2  // TINYINT
	ValueTypeSmallint          = 3  // SMALLINT
	ValueTypeInteger           = 4  // INTEGER
	ValueTypeBigint            = 5  // BIGINT
	ValueTypeNumeric           = 6  // NUMERIC / DECIMAL
	ValueTypeDouble            = 7  // DOUBLE PRECISION
	ValueTypeReal              = 8  // REAL / FLOAT(24)
	ValueTypeTime              = 9  // TIME
	ValueTypeDate              = 10 // DATE
	ValueTypeTimestamp         = 11 // TIMESTAMP
	ValueTypeVarbinary         = 12 // VARBINARY
	ValueTypeVarchar           = 13 // VARCHAR
	ValueTypeVarcharIgnoreCase = 14 // VARCHAR_IGNORECASE
	ValueTypeBlob              = 15 // BLOB
	ValueTypeClob              = 16 // CLOB
	ValueTypeArray             = 17 // ARRAY
	ValueTypeJavaObject        = 19 // JAVA_OBJECT
	ValueTypeUUID              = 20 // UUID
	ValueTypeChar              = 21 // CHAR
	ValueTypeGeometry          = 22 // GEOMETRY
	ValueTypeTimestampTZ       = 24 // TIMESTAMP WITH TIME ZONE
	ValueTypeEnum              = 25 // ENUM
	ValueTypeInterval          = 26 // INTERVAL
	ValueTypeRow               = 27 // ROW
	ValueTypeJSON              = 28 // JSON
	ValueTypeTimeTZ            = 29 // TIME WITH TIME ZONE
	ValueTypeBinary            = 30 // BINARY
	ValueTypeDecfloat          = 31 // DECFLOAT
)
