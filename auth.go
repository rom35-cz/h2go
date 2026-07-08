package h2go

import (
	"crypto/sha256"
	"strings"
	"unicode/utf16"
)

// userPasswordHash computes the H2-compatible user password hash.
//
// The hash is SHA-256 over the big-endian UTF-16 encoding of:
//
//	UPPER_ENGLISH(user) + "@" + password
//
// where each Java char (UTF-16 code unit) is serialized as two bytes:
//
//	byte[0] = high 8 bits of the code unit
//	byte[1] = low 8 bits of the code unit
//
// An empty user name together with an empty password produces an empty
// hash (a zero-length byte slice).
//
// Go's strings are immutable, so the inputs cannot be zeroed from memory
// after use (unlike Java's mutable char[]).
func userPasswordHash(user, password string) []byte {
	if user == "" && password == "" {
		return []byte{}
	}
	return keyPasswordHash(strings.ToUpper(user), password)
}

// filePasswordHash returns the H2-compatible file password hash, or nil
// when filePassword is empty.
//
// Unlike userPasswordHash, the fixed prefix user name "file" is NOT
// uppercased, matching the Java side in ConnectionInfo.convertPasswords
// which calls hashPassword(passwordHash, "file", filePassword) directly.
func filePasswordHash(filePassword string) []byte {
	if filePassword == "" {
		return nil
	}
	return keyPasswordHash("file", filePassword)
}

// keyPasswordHash mirrors SHA256.getKeyPasswordHash.
//
// It concatenates userName + "@" + password, encodes the result as
// UTF-16 code units, serialises each code unit in big-endian order, and
// returns the SHA-256 digest.
func keyPasswordHash(userName, password string) []byte {
	u := utf16.Encode([]rune(userName + "@"))
	p := utf16.Encode([]rune(password))
	buf := make([]byte, 0, (len(u)+len(p))*2)
	for _, c := range u {
		buf = append(buf, byte(c>>8), byte(c))
	}
	for _, c := range p {
		buf = append(buf, byte(c>>8), byte(c))
	}
	h := sha256.Sum256(buf)
	return h[:]
}
