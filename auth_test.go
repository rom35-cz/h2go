package h2go

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// ---- Known-answer vectors (computed from the H2 algorithm) ----

func TestUserPasswordHash_Empty(t *testing.T) {
	h := userPasswordHash("", "")
	if len(h) != 0 {
		t.Fatalf("empty hash length = %d, want 0", len(h))
	}
}

func TestUserPasswordHash_Golden(t *testing.T) {
	cases := []struct {
		user, pw, wantHex string
	}{
		// user="SA" pw="" → SHA256(UTF16-BE("SA@"))
		{"SA", "", "028dc3ecc357eb59b1b98e1450fe7cc035510bf3c011d5f32dab495d927751a2"},
		// user="SA" pw="password" → SHA256(UTF16-BE("SA@password"))
		{"SA", "password", "51acc375c9819b14ae1b84004b6e23eeee7b5af9fbad97e7eb4beaee11c2498d"},
		// user="sa" pw="pass" → SHA256(UTF16-BE("SA@pass"))
		{"sa", "pass", "91be7da2645562c37bdb7354e8e3dbd1b9114e26fc5289135c0032dbf22294f4"},
		// user="ROOT" pw="toor" → SHA256(UTF16-BE("ROOT@toor"))
		{"ROOT", "toor", "2e3b2be7ac8febf57199d7e257137743db24c022e8c80c9b664a4454c43fc1f0"},
		// user="User1" pw="secret!" → SHA256(UTF16-BE("USER1@secret!"))
		{"User1", "secret!", "2616d05f7e665a655c082c69ad7c0451c3561b3b4aedc1d684fde2ea302a6521"},
	}
	for _, tc := range cases {
		t.Run(tc.user+"_"+tc.pw, func(t *testing.T) {
			want, err := hex.DecodeString(tc.wantHex)
			if err != nil {
				t.Fatalf("bad hex in test data: %v", err)
			}
			got := userPasswordHash(tc.user, tc.pw)
			if !bytes.Equal(got, want) {
				t.Fatalf("hash mismatch\ngot  = %x\nwant = %x", got, want)
			}
		})
	}
}

func TestUserPasswordHash_UppercaseNormalisation(t *testing.T) {
	// H2 uppercases the user name with English/ASCII rules before hashing.
	hUpper := userPasswordHash("SA", "password")
	hLower := userPasswordHash("sa", "password")
	hMixed := userPasswordHash("Sa", "password")
	if !bytes.Equal(hUpper, hLower) || !bytes.Equal(hUpper, hMixed) {
		t.Fatal("mixed-case usernames must normalise to the same hash")
	}
}

func TestUserPasswordHash_DifferentPasswords(t *testing.T) {
	h1 := userPasswordHash("SA", "alpha")
	h2 := userPasswordHash("SA", "beta")
	if bytes.Equal(h1, h2) {
		t.Fatal("different passwords must not produce the same hash")
	}
}

func TestUserPasswordHash_Length(t *testing.T) {
	for _, tc := range []struct{ user, pw string }{
		{"SA", ""},
		{"SA", "x"},
		{"", "x"},
	} {
		h := userPasswordHash(tc.user, tc.pw)
		if len(h) != 32 {
			t.Fatalf("hash length = %d, want 32", len(h))
		}
	}
}

func TestUserPasswordHash_Deterministic(t *testing.T) {
	h1 := userPasswordHash("admin", "admin")
	h2 := userPasswordHash("admin", "admin")
	if !bytes.Equal(h1, h2) {
		t.Fatal("hash must be deterministic")
	}
}

// ---- File password hash ----

func TestFilePasswordHash_Empty(t *testing.T) {
	h := filePasswordHash("")
	if h != nil {
		t.Fatalf("filePasswordHash(\"\") = %x, want nil", h)
	}
}

func TestFilePasswordHash_Golden(t *testing.T) {
	cases := []struct {
		pw, wantHex string
	}{
		// filePassword="secret" → keyPasswordHash("file", "secret")
		{"secret", "19bd73c7b6dd777acd9913bfe1d07401af7f6936707aa1fbccfca731328dae2a"},
		// filePassword="h2pass" → keyPasswordHash("file", "h2pass")
		{"h2pass", "e183a56b4b8231abd9dbcb93f1b5d143caf385c0811132f05232c28c43437db9"},
	}
	for _, tc := range cases {
		t.Run(tc.pw, func(t *testing.T) {
			want, err := hex.DecodeString(tc.wantHex)
			if err != nil {
				t.Fatalf("bad hex in test data: %v", err)
			}
			got := filePasswordHash(tc.pw)
			if !bytes.Equal(got, want) {
				t.Fatalf("hash mismatch\ngot  = %x\nwant = %x", got, want)
			}
		})
	}
}

// The fixed prefix for file passwords is "file" (lower case). It must NOT be
// uppercased, unlike regular user names.
func TestFilePasswordHash_NotUppercased(t *testing.T) {
	hFile := filePasswordHash("secret")
	hUpper := userPasswordHash("file", "secret")
	if bytes.Equal(hFile, hUpper) {
		t.Fatal("file password hash must NOT uppercase the \"file\" prefix")
	}
}

func TestFilePasswordHash_Deterministic(t *testing.T) {
	h1 := filePasswordHash("s3cr3t")
	h2 := filePasswordHash("s3cr3t")
	if !bytes.Equal(h1, h2) {
		t.Fatal("file password hash must be deterministic")
	}
}

// ---- Byte layout verification ----

// Test that the raw UTF-16-BE byte layout before hashing is exactly what H2
// produces: each char (UTF-16 code unit) as two big-endian bytes.
func TestKeyPasswordHash_ByteLayout(t *testing.T) {
	// "SA@" in UTF-16-BE = 0x00 0x53 0x00 0x41 0x00 0x40
	wantRaw := []byte{0x00, 0x53, 0x00, 0x41, 0x00, 0x40}
	h := keyPasswordHash("SA", "")
	wantHash, _ := hex.DecodeString("028dc3ecc357eb59b1b98e1450fe7cc035510bf3c011d5f32dab495d927751a2")
	if !bytes.Equal(h, wantHash) {
		t.Fatalf("hash of raw layout mismatch; verify the UTF-16-BE encoding\nraw  = %x\ngot  = %x\nwant = %x", wantRaw, h, wantHash)
	}
}
