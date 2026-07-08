-- h2-data/seed.sql
-- Integration-test seed for the h2-go driver.
--
-- Idempotent: drops every table and recreates it from scratch so the
-- script is safe to run multiple times.
--
-- Run via:  make db-seed          (server must be running: cd h2-data && ./h2.sh)
-- Tool:     org.h2.tools.RunScript over JDBC TCP


-- =========================================================================
-- Teardown (reverse FK order)
-- =========================================================================
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS type_showcase;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS people;
DROP TABLE IF EXISTS GO_DRIVER_TEST;   -- leftover from early handshake tests


-- =========================================================================
-- people
-- Exercises: INTEGER PK / AUTO_INCREMENT, VARCHAR (nullable), INTEGER
--            (nullable), BOOLEAN, DOUBLE (nullable), CLOB (nullable),
--            TIMESTAMP with DEFAULT NOW().
-- Rows include NULLs in every nullable column at least once.
-- =========================================================================
CREATE TABLE people (
    id         INTEGER      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    email      VARCHAR(200),           -- nullable: row 3 is NULL
    age        INTEGER,                -- nullable: row 4 is NULL
    active     BOOLEAN      NOT NULL DEFAULT TRUE,
    score      DOUBLE,                 -- nullable: rows 3 and 4 are NULL
    notes      CLOB,                   -- nullable: rows 2, 5 are NULL
    created_at TIMESTAMP    NOT NULL DEFAULT NOW()
);

INSERT INTO people (name, email, age, active, score, notes) VALUES
    ('Alice',   'alice@example.com', 30,   TRUE,  9.5,  'first user'),
    ('Bob',     'bob@example.com',   25,   TRUE,  7.2,  NULL),
    ('Charlie', NULL,                40,   FALSE, NULL, 'no email on file'),
    ('Diana',   'diana@example.com', NULL, TRUE,  8.8,  'age unknown'),
    ('Eve',     'eve@example.com',   35,   FALSE, 6.1,  NULL);


-- =========================================================================
-- products
-- Exercises: BIGINT PK / AUTO_INCREMENT, VARCHAR UNIQUE, DECIMAL(10,2),
--            INTEGER DEFAULT, CLOB (nullable).
-- =========================================================================
CREATE TABLE products (
    id          BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    sku         VARCHAR(50)   NOT NULL UNIQUE,
    name        VARCHAR(100)  NOT NULL,
    price       DECIMAL(10,2) NOT NULL,
    stock       INTEGER       NOT NULL DEFAULT 0,
    description CLOB                   -- nullable: rows 2 and 4 are NULL
);

INSERT INTO products (sku, name, price, stock, description) VALUES
    ('WIDGET-A', 'Widget A',    9.99, 100, 'A standard widget'),
    ('WIDGET-B', 'Widget B',   19.99,  50, NULL),
    ('GADGET-X', 'Gadget X',  149.99,  10, 'Premium gadget'),
    ('GADGET-Y', 'Gadget Y',   99.50,   0, NULL),
    ('GIZMO-1',  'Gizmo 1',    1.00,  999, 'Budget gizmo');


-- =========================================================================
-- orders
-- Exercises: BIGINT PK, FK → INTEGER / BIGINT, DECIMAL(12,2),
--            TIMESTAMP DEFAULT NOW(), TIMESTAMP nullable.
-- shipped_at is NULL on unshipped orders.
-- =========================================================================
CREATE TABLE orders (
    id          BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    person_id   INTEGER       NOT NULL REFERENCES people(id),
    product_id  BIGINT        NOT NULL REFERENCES products(id),
    quantity    INTEGER       NOT NULL DEFAULT 1,
    total_price DECIMAL(12,2) NOT NULL,
    ordered_at  TIMESTAMP     NOT NULL DEFAULT NOW(),
    shipped_at  TIMESTAMP               -- nullable: not yet shipped
);

INSERT INTO orders (person_id, product_id, quantity, total_price, shipped_at) VALUES
    (1, 1, 2,   19.98, NOW()),          -- Alice bought 2× Widget A, shipped
    (1, 3, 1,  149.99, NULL),           -- Alice bought Gadget X, not shipped
    (2, 2, 3,   59.97, NOW()),          -- Bob bought 3× Widget B, shipped
    (4, 5, 10,  10.00, NULL),           -- Diana bought 10× Gizmo 1, not shipped
    (3, 1, 1,    9.99, NOW());          -- Charlie bought 1× Widget A, shipped


-- =========================================================================
-- type_showcase
-- One column per ValueType constant defined in protocol.go.
-- Three rows:
--   id=1  typical / maximum values
--   id=2  minimum / edge / zero values
--   id=3  all nullable columns are NULL  (NULL path for every decoder)
--
-- Types covered (by ValueType code):
--   0  NULL            → col_null_int   (always NULL)
--   1  BOOLEAN         → col_boolean_t, col_boolean_f
--   2  TINYINT         → col_tinyint
--   3  SMALLINT        → col_smallint
--   4  INTEGER         → col_integer
--   5  BIGINT          → col_bigint
--   6  NUMERIC/DECIMAL → col_decimal
--   7  DOUBLE          → col_double
--   8  REAL            → col_real
--   9  TIME            → col_time
--  10  DATE            → col_date
--  11  TIMESTAMP       → col_timestamp
--  12  VARBINARY       → col_varbinary
--  13  VARCHAR         → col_varchar
--  14  VARCHAR_IC      → col_varchar_ic
--  15  BLOB            → col_blob
--  16  CLOB            → col_clob
--  20  UUID            → col_uuid
--  21  CHAR            → col_char
--  24  TIMESTAMP TZ    → col_timestamp_tz
--  28  JSON            → col_json
--  29  TIME TZ         → col_time_tz
--  30  BINARY          → col_binary
--  31  DECFLOAT        → col_decfloat
-- =========================================================================
CREATE TABLE type_showcase (
    id               INTEGER          NOT NULL PRIMARY KEY,

    -- integer types
    col_tinyint      TINYINT,
    col_smallint     SMALLINT,
    col_integer      INTEGER,
    col_bigint       BIGINT,

    -- floating point
    col_real         REAL,
    col_double       DOUBLE,
    col_decimal      DECIMAL(15,5),
    col_decfloat     DECFLOAT,

    -- boolean (two columns: one true, one false)
    col_boolean_t    BOOLEAN,
    col_boolean_f    BOOLEAN,

    -- string
    col_varchar      VARCHAR(200),
    col_char         CHAR(10),
    col_varchar_ic   VARCHAR_IGNORECASE(100),

    -- binary
    col_binary       BINARY(4),
    col_varbinary    VARBINARY(100),

    -- large objects
    col_clob         CLOB,
    col_blob         BLOB,

    -- date / time
    col_date         DATE,
    col_time         TIME,
    col_time_tz      TIME WITH TIME ZONE,
    col_timestamp    TIMESTAMP,
    col_timestamp_tz TIMESTAMP WITH TIME ZONE,

    -- other
    col_uuid         UUID,
    col_json         JSON,

    -- always NULL: exercises the NULL decoder for every consuming phase
    col_null_int     INTEGER
);

-- Row 1: typical / maximum values
INSERT INTO type_showcase VALUES (
    1,
    127,                                                 -- TINYINT  max
    32767,                                               -- SMALLINT max
    2147483647,                                          -- INTEGER  max
    9223372036854775807,                                 -- BIGINT   max
    3.14,                                                -- REAL
    2.718281828459045,                                   -- DOUBLE
    12345.67890,                                         -- DECIMAL(15,5)
    3.14159265358979323846,                              -- DECFLOAT (higher precision)
    TRUE,                                                -- BOOLEAN true
    FALSE,                                               -- BOOLEAN false
    'hello, world',                                      -- VARCHAR
    'CHAR      ',                                        -- CHAR(10) — padded to 10 chars
    'Mixed CASE value',                                  -- VARCHAR_IGNORECASE
    X'DEADBEEF',                                         -- BINARY(4)
    X'CAFEBABE01020304',                                 -- VARBINARY
    'large clob content for testing',                    -- CLOB
    X'0102030405060708',                                 -- BLOB
    DATE '2024-01-15',
    TIME '13:45:00',
    CAST('13:45:00+02:00'   AS TIME WITH TIME ZONE),
    TIMESTAMP '2024-01-15 13:45:00',
    CAST('2024-01-15 13:45:00+02:00' AS TIMESTAMP WITH TIME ZONE),
    '550e8400-e29b-41d4-a716-446655440000',              -- UUID
    '{"key": "value", "num": 42}',                       -- JSON
    NULL
);

-- Row 2: minimum / edge / zero values
INSERT INTO type_showcase VALUES (
    2,
    -128,                                                -- TINYINT  min
    -32768,                                              -- SMALLINT min
    -2147483648,                                         -- INTEGER  min
    -9223372036854775807,                                -- BIGINT   near-min
    -3.14,                                               -- REAL negative
    -2.718281828459045,                                  -- DOUBLE negative
    -12345.67890,                                        -- DECIMAL negative
    -3.14159265358979323846,                             -- DECFLOAT negative
    FALSE,                                               -- BOOLEAN false
    TRUE,                                                -- BOOLEAN true
    '',                                                  -- VARCHAR empty
    '          ',                                        -- CHAR(10) all spaces
    '',                                                  -- VARCHAR_IGNORECASE empty
    X'00000000',                                         -- BINARY(4) zeros
    X'00',                                               -- VARBINARY single zero byte
    '',                                                  -- CLOB empty
    X'00',                                               -- BLOB single zero byte
    DATE '1970-01-01',
    TIME '00:00:00',
    CAST('00:00:00+00:00'   AS TIME WITH TIME ZONE),
    TIMESTAMP '1970-01-01 00:00:00',
    CAST('1970-01-01 00:00:00+00:00' AS TIMESTAMP WITH TIME ZONE),
    '00000000-0000-0000-0000-000000000000',              -- UUID nil
    '{}',                                                -- JSON empty object
    NULL
);

-- Row 3: all nullable columns NULL — exercises the NULL decoder path for every type
INSERT INTO type_showcase (id) VALUES (3);


-- =========================================================================
-- Sanity check counts (printed by RunScript with -showResults)
-- =========================================================================
SELECT 'people'        AS tbl, COUNT(*) AS rows FROM people        UNION ALL
SELECT 'products',             COUNT(*)         FROM products       UNION ALL
SELECT 'orders',               COUNT(*)         FROM orders         UNION ALL
SELECT 'type_showcase',        COUNT(*)         FROM type_showcase;
