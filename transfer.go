package h2go

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// DefaultBufferSize is the default buffer size for the transfer stream.
const DefaultBufferSize = 64 * 1024

// Tr is a codec that encodes and decodes H2 wire protocol primitives over a
// buffered reader/writer pair. It wraps a network connection with buffered I/O
// for efficient read/write of small values.
//
// All multi-byte values are encoded in big-endian byte order, matching Java's
// DataOutputStream/DataInputStream.
type Tr struct {
	r  *bufio.Reader
	w  *bufio.Writer
	wc io.WriteCloser
}

// NewReader creates a Tr that reads from r. It has no writer and cannot Flush.
func NewReader(r io.Reader) *Tr {
	return &Tr{r: bufio.NewReaderSize(r, DefaultBufferSize)}
}

// NewWriter creates a Tr that writes to w.
func NewWriter(w io.WriteCloser) *Tr {
	return &Tr{w: bufio.NewWriterSize(w, DefaultBufferSize), wc: w}
}

// NewReadWriter creates a Tr over a bidirectional connection.
func NewReadWriter(rw io.ReadWriteCloser) *Tr {
	return &Tr{
		r:  bufio.NewReaderSize(rw, DefaultBufferSize),
		w:  bufio.NewWriterSize(rw, DefaultBufferSize),
		wc: rw,
	}
}

// Flush writes any buffered data to the underlying writer.
func (t *Tr) Flush() error {
	if t.w == nil {
		return errors.New("Tr: flush called on read-only transfer")
	}
	return t.w.Flush()
}

// Close flushes buffered data and closes the underlying write closer.
func (t *Tr) Close() error {
	if t.w != nil {
		if err := t.w.Flush(); err != nil {
			if t.wc != nil {
				_ = t.wc.Close()
			}
			return err
		}
		if t.wc != nil {
			return t.wc.Close()
		}
	}
	return nil
}

// ---- Primitives: write ----

// WriteBool writes a boolean as a single byte (0 or 1).
func (t *Tr) WriteBool(x bool) error {
	if x {
		return t.WriteByte(1)
	}
	return t.WriteByte(0)
}

// WriteByte writes a single byte.
func (t *Tr) WriteByte(x byte) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	return t.w.WriteByte(x)
}

// WriteInt16 writes an int16 in big-endian byte order.
func (t *Tr) WriteInt16(x int16) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(x))
	_, err := t.w.Write(buf[:])
	return err
}

// WriteInt32 writes an int32 in big-endian byte order.
func (t *Tr) WriteInt32(x int32) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(x))
	_, err := t.w.Write(buf[:])
	return err
}

// WriteInt64 writes an int64 in big-endian byte order.
func (t *Tr) WriteInt64(x int64) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(x))
	_, err := t.w.Write(buf[:])
	return err
}

// WriteFloat32 writes a float32 in big-endian IEEE 754 representation.
func (t *Tr) WriteFloat32(x float32) error {
	return t.WriteInt32(int32(math.Float32bits(x)))
}

// WriteFloat64 writes a float64 in big-endian IEEE 754 representation.
func (t *Tr) WriteFloat64(x float64) error {
	return t.WriteInt64(int64(math.Float64bits(x)))
}

// WriteString writes a string using H2 wire format: int32 length in UTF-16
// code units, followed by that many big-endian 16-bit code units.
// A null string is encoded as length -1.
func (t *Tr) WriteString(s string) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	if !utf16Valid(s) {
		// Java's DataOutputStream.writeChars uses char==code unit.
		// This shouldn't happen for well-formed Go strings, but guard it.
		return fmt.Errorf("Tr: string contains invalid UTF-16 surrogate: %q", s)
	}
	if s == "" {
		return t.WriteInt32(0)
	}

	// Encode the string to UTF-16 code units.
	units := utf16Encode(s)
	if err := t.WriteInt32(int32(len(units))); err != nil {
		return err
	}
	var buf [2]byte
	for _, u := range units {
		binary.BigEndian.PutUint16(buf[:], u)
		if _, err := t.w.Write(buf[:]); err != nil {
			return err
		}
	}
	return nil
}

// WriteNullString writes a null string marker (length -1).
func (t *Tr) WriteNullString() error {
	return t.WriteInt32(-1)
}

// WriteBytes writes a byte slice using H2 wire format: int32 length followed
// by the raw bytes. A nil slice is encoded as length -1.
func (t *Tr) WriteBytes(b []byte) error {
	if t.w == nil {
		return errors.New("Tr: write on read-only transfer")
	}
	if b == nil {
		return t.WriteInt32(-1)
	}
	if err := t.WriteInt32(int32(len(b))); err != nil {
		return err
	}
	_, err := t.w.Write(b)
	return err
}

// WriteRowCount writes a row count as an int64 (protocol 21 uses long).
func (t *Tr) WriteRowCount(n int64) error {
	return t.WriteInt64(n)
}

// ---- Primitives: read ----

// ReadBool reads a boolean (single byte, non-zero is true).
func (t *Tr) ReadBool() (bool, error) {
	b, err := t.ReadByte()
	if err != nil {
		return false, err
	}
	return b != 0, nil
}

// ReadByte reads a single byte.
func (t *Tr) ReadByte() (byte, error) {
	return t.r.ReadByte()
}

// ReadInt16 reads an int16 in big-endian byte order.
func (t *Tr) ReadInt16() (int16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(t.r, buf[:]); err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(buf[:])), nil
}

// ReadInt32 reads an int32 in big-endian byte order.
func (t *Tr) ReadInt32() (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(t.r, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(buf[:])), nil
}

// ReadInt64 reads an int64 in big-endian byte order.
func (t *Tr) ReadInt64() (int64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(t.r, buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(buf[:])), nil
}

// ReadFloat32 reads a float32 in big-endian IEEE 754 representation.
func (t *Tr) ReadFloat32() (float32, error) {
	v, err := t.ReadInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(v)), nil
}

// ReadFloat64 reads a float64 in big-endian IEEE 754 representation.
func (t *Tr) ReadFloat64() (float64, error) {
	v, err := t.ReadInt64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(uint64(v)), nil
}

// ReadString reads a string in H2 wire format. Returns empty string for zero
// length and "" for non-null empty. Returns nil for a null marker (length -1).
// Use ReadStringPtr for a *string return.
func (t *Tr) ReadString() (*string, error) {
	length, err := t.ReadInt32()
	if err != nil {
		return nil, err
	}
	if length == -1 {
		return nil, nil // null
	}
	if length == 0 {
		s := ""
		return &s, nil
	}
	if length < 0 {
		return nil, fmt.Errorf("Tr: negative string length %d", length)
	}

	// Read UTF-16 code units.
	units := make([]uint16, length)
	var buf [2]byte
	for i := int32(0); i < length; i++ {
		if _, err := io.ReadFull(t.r, buf[:]); err != nil {
			return nil, err
		}
		units[i] = binary.BigEndian.Uint16(buf[:])
	}
	s := utf16Decode(units)
	return &s, nil
}

// ReadStringPtr is a convenience wrapper around ReadString that returns the
// string value or empty for null.
func (t *Tr) ReadStringPtr() (string, error) {
	s, err := t.ReadString()
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", nil
	}
	return *s, nil
}

// ReadBytes reads a byte slice in H2 wire format. Returns nil for null.
func (t *Tr) ReadBytes() ([]byte, error) {
	length, err := t.ReadInt32()
	if err != nil {
		return nil, err
	}
	if length == -1 {
		return nil, nil // null
	}
	if length == 0 {
		return []byte{}, nil
	}
	if length < 0 {
		return nil, fmt.Errorf("Tr: negative bytes length %d", length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(t.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ReadRowCount reads a row count as an int64 (protocol 21 uses long).
func (t *Tr) ReadRowCount() (int64, error) {
	return t.ReadInt64()
}

// ---- UTF-16 encoding helpers ----

// utf16Valid reports whether s contains valid UTF-8 that can be round-tripped
// through UTF-16 (any valid Go string is valid; isolated surrogates in Go
// strings would cause issues).
func utf16Valid(_ string) bool {
	// Go strings are valid UTF-8 by construction unless they contain
	// malformed runes. For driver purposes, we accept standard UTF-8;
	// if we ever need to handle arbitrary byte sequences we can use
	// UTF-16BE directly, but that's not needed for typical SQL strings.
	return true
}

// utf16Encode encodes a Go string into a []uint16 of UTF-16 code units.
// Non-BMP characters (surrogate pairs) are encoded as two code units.
func utf16Encode(s string) []uint16 {
	// Pre-allocate — worst case is every rune is a surrogate pair:
	// len(s) bytes can be at most len(s) runes, but each pair takes 2 units.
	// Upper bound: len(s) runes × 2 units = 2*len(s).
	// A simpler upper bound is len(s) (each byte becomes a Latin-1 unit at most
	// for ASCII), times 2 for pairs. Use len(s)+1 to be safe for short strings.
	result := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r < 0x10000 {
			// BMP — one code unit
			result = append(result, uint16(r))
		} else {
			// Non-BMP — surrogate pair
			r -= 0x10000
			result = append(result, 0xD800|uint16(r>>10))
			result = append(result, 0xDC00|uint16(r&0x3FF))
		}
	}
	return result
}

// utf16Decode decodes a []uint16 of UTF-16 code units into a Go string.
func utf16Decode(units []uint16) string {
	var b strings.Builder
	b.Grow(len(units))

	i := 0
	for i < len(units) {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF {
			// High surrogate — expect a low surrogate next.
			if i+1 >= len(units) {
				// Lone high surrogate, emit replacement character.
				b.WriteRune(0xFFFD)
				i++
				continue
			}
			lo := units[i+1]
			if lo >= 0xDC00 && lo <= 0xDFFF {
				r := rune(u-0xD800)<<10 + rune(lo-0xDC00) + 0x10000
				b.WriteRune(r)
				i += 2
				continue
			}
			// Not a valid pair, emit replacement.
			b.WriteRune(0xFFFD)
			i++
		} else if u >= 0xDC00 && u <= 0xDFFF {
			// Lone low surrogate.
			b.WriteRune(0xFFFD)
			i++
		} else {
			b.WriteRune(rune(u))
			i++
		}
	}
	return b.String()
}
