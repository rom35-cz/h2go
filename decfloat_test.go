// decfloat_test.go — unit tests for exact DECFLOAT parsing/rendering and the
// wire-level fail-fast validation (post-v0.2.0 backlog item #3).
//
// Rendering goldens follow java.math.BigDecimal.toString() semantics, which
// is what ValueDecfloat.getString() puts on the wire; live H2 ground truth
// for tricky forms is asserted separately in the integration test.

package h2go

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseDecFloatMatrix(t *testing.T) {
	tests := []struct {
		in        string
		wantStr   string  // canonical rendering; "" means expect parse error
		wantFloat float64 // checked only when wantStr != ""
	}{
		// Specials.
		{"Infinity", "Infinity", 0},
		{"-Infinity", "-Infinity", 0},
		{"NaN", "NaN", 0},

		// Plain forms.
		{"0", "0", 0},
		{"-0", "0", 0}, // big.Int has no negative zero
		{"123.456", "123.456", 123.456},
		{"+123.456", "123.456", 123.456},
		{"-123.456", "-123.456", -123.456},
		{"5.", "5", 5},        // trailing dot accepted by BigDecimal grammar
		{".25", "0.25", 0.25}, // leading dot accepted
		{"000123", "123", 123},

		// Exponent forms.
		{"1E+7", "1E+7", 1e7},
		{"1e7", "1E+7", 1e7},
		{"1E-7", "1E-7", 1e-7},
		{"1.5E-25", "1.5E-25", 1.5e-25},
		{"123E-5", "0.00123", 0.00123}, // falls back into plain window
		{"12134567890E+3", "1.2134567890E+13", 1.213456789e13},
		{"0.00", "0.00", 0}, // zero keeps its scale in plain window
		{"0E+3", "0E+3", 0}, // negative scale stays scientific

		// Rejections (BigDecimal grammar violations / garbage frames).
		{"", "", 0},
		{".", "", 0},
		{"abc", "", 0},
		{"--1", "", 0},
		{"1.2.3", "", 0},
		{"1 e5", "", 0},
		{"5e", "", 0},
		{"1E+5x", "", 0},
		{"1 E5", "", 0},
		{"infinity", "", 0}, // specials are case-sensitive
	}
	for _, tc := range tests {
		df, err := ParseDecFloat(tc.in)
		if tc.wantStr == "" {
			if err == nil {
				t.Errorf("ParseDecFloat(%q): expected error, got %v", tc.in, df)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDecFloat(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got := df.String(); got != tc.wantStr {
			t.Errorf("ParseDecFloat(%q).String() = %q, want %q", tc.in, got, tc.wantStr)
		}
		if !strings.ContainsAny(tc.wantStr, "IN") { // skip specials' float check
			if f, err := df.Float64(); err != nil {
				t.Errorf("ParseDecFloat(%q).Float64(): %v", tc.in, err)
			} else if f != tc.wantFloat {
				t.Errorf("ParseDecFloat(%q).Float64() = %v, want %v", tc.in, f, tc.wantFloat)
			}
		}
	}
}

func TestDecFloatSpecials(t *testing.T) {
	pos, _ := ParseDecFloat("Infinity")
	neg, _ := ParseDecFloat("-Infinity")
	nan, _ := ParseDecFloat("NaN")
	fin, _ := ParseDecFloat("12.5")

	if !pos.IsInf(1) || pos.IsInf(-1) || !pos.IsInf(0) {
		t.Error("Infinity: IsInf sign handling wrong")
	}
	if !neg.IsInf(-1) || neg.IsInf(1) {
		t.Error("-Infinity: IsInf sign handling wrong")
	}
	if !nan.IsNaN() || nan.IsInf(0) || nan.IsFinite() {
		t.Error("NaN: predicates wrong")
	}
	if fin.IsNaN() || fin.IsInf(0) || !fin.IsFinite() {
		t.Error("12.5: predicates wrong")
	}
	var zero DecFloat
	if !zero.IsFinite() || zero.String() != "0" {
		t.Error("zero DecFloat: want finite exact 0")
	}
}

func TestDecFloatExactness(t *testing.T) {
	// 40 significant digits — far beyond float64's ~17 — must round-trip
	// exactly through String().
	const digits = "1234567890123456789012345678901234567890"
	df, err := ParseDecFloat(digits + ".42")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := df.String(); got != digits+".42" {
		t.Errorf("exact round-trip lost: got %q", got)
	}
	if f, err := df.Float64(); err != nil || f == 0 {
		t.Errorf("Float64 conversion unexpected: %v %v", f, err)
	}
}

func TestDecFloatScanValue(t *testing.T) {
	var df DecFloat
	if err := df.Scan(nil); err != nil || df.String() != "0" {
		t.Errorf("Scan(nil): %v %q", err, df.String())
	}
	if err := df.Scan("1.25"); err != nil || df.String() != "1.25" {
		t.Errorf("Scan(string): %v %q", err, df.String())
	}
	if err := df.Scan([]byte("NaN")); err != nil || !df.IsNaN() {
		t.Errorf("Scan([]byte): %v NaN=%v", err, df.IsNaN())
	}
	if err := df.Scan(42); err == nil {
		t.Error("Scan(int): expected unsupported-type error")
	}
	if err := df.Scan("garbage"); err == nil {
		t.Error("Scan(garbage): expected parse error")
	}

	v, err := df.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	if s, ok := v.(string); !ok || s != "NaN" {
		t.Errorf("Value() = (%T,%v), want string NaN", v, v)
	}
}

func TestReadValueInvalidDecfloatRejected(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeDecfloat)
	writeString(buf, "12.34.56")

	tr := mockTransferFromBytes(buf.Bytes())
	_, err := tr.ReadValue(nil)
	if err == nil {
		t.Fatal("expected invalid DECFLOAT error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid DECFLOAT") {
		t.Errorf("error %q should mention \"invalid DECFLOAT\"", err)
	}
}

func TestReadValueValidDecfloatPassesThrough(t *testing.T) {
	buf := new(bytes.Buffer)
	writeValueType(buf, ValueTypeDecfloat)
	writeString(buf, "1.5E-25")

	tr := mockTransferFromBytes(buf.Bytes())
	v, err := tr.ReadValue(nil)
	if err != nil {
		t.Fatalf("ReadValue: %v", err)
	}
	if s, ok := v.(string); !ok || s != "1.5E-25" {
		t.Errorf("ReadValue = (%T,%v), want string \"1.5E-25\"", v, v)
	}
}
