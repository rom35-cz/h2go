// decfloat.go — exact DECFLOAT support.
//
// Wire format (protocol 21, Transfer.java): DECFLOAT values are transferred
// as their string representation — writeValue writes writeString(v.getString())
// and the reference client reads them back with new BigDecimal(s), mapping
// the three special spellings to ValueDecfloat.POSITIVE_INFINITY,
// NEGATIVE_INFINITY and NAN. Finite values therefore follow
// java.math.BigDecimal.toString(): scientific notation is used exactly when
// the scale is negative or the adjusted exponent (precision − scale − 1) is
// less than −6, and the exponent letter is followed by an explicit sign.
//
// DecFloat mirrors BigDecimal's exact (unscaled × 10^−scale) representation
// so DECFLOAT values round-trip byte-for-byte without float64 lossiness.
// Rows.Scan into a *DecFloat opts in; scanning into interface{} still yields
// the raw (validated) string, matching H2's own textual rendering.

package h2go

import (
	"database/sql/driver"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type decSpecial uint8

const (
	decFinite decSpecial = iota
	decPosInf
	decNegInf
	decNaN
)

// DecFloat holds an exact decimal floating-point value: unscaled × 10^−scale,
// mirroring java.math.BigDecimal, plus the DECFLOAT special values
// +Infinity, −Infinity and NaN. The zero value represents exact 0.
//
// It implements database/sql Scanner and driver.Valuer so it can be scanned
// from and bound to DECFLOAT (or NUMERIC/VARCHAR) columns directly:
//
//	var df h2go.DecFloat
//	row := db.QueryRow("SELECT dec_col FROM t WHERE id = ?", 1)
//	if err := row.Scan(&df); err != nil { ... }
//	fmt.Println(df.String()) // exact textual form, byte-identical to H2's
//
// Use ParseDecFloat to build one from its textual form.
type DecFloat struct {
	special  decSpecial
	unscaled *big.Int // significant digits including sign; nil means 0
	scale    int32    // value = unscaled × 10^−scale; meaningful when finite
}

// ParseDecFloat parses the textual representation of a DECFLOAT value: the
// java.math.BigDecimal grammar ([sign] digits [. digits] [eE [sign] digits],
// no whitespace, forms like "5.", ".25" and "1.5E-25" accepted) plus the
// special spellings "Infinity", "-Infinity" and "NaN" that DECFLOAT adds on
// top of BigDecimal.
func ParseDecFloat(s string) (DecFloat, error) {
	switch s {
	case "Infinity":
		return DecFloat{special: decPosInf}, nil
	case "-Infinity":
		return DecFloat{special: decNegInf}, nil
	case "NaN":
		return DecFloat{special: decNaN}, nil
	}

	i, n := 0, len(s)
	neg := false
	if i < n && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	intStart := i
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intDigits := s[intStart:i]
	var fracDigits string
	if i < n && s[i] == '.' {
		i++
		fracStart := i
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		fracDigits = s[fracStart:i]
	}
	digits := intDigits + fracDigits
	if digits == "" {
		return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: no digits in %q", s)
	}

	exp := 0
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		expNeg := false
		if i < n && (s[i] == '+' || s[i] == '-') {
			expNeg = s[i] == '-'
			i++
		}
		expStart := i
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == expStart {
			return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: exponent digits missing in %q", s)
		}
		if len(s)-expStart > 10 {
			return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: exponent out of range in %q", s)
		}
		v, _ := strconv.Atoi(s[expStart:i])
		if expNeg {
			v = -v
		}
		exp = v
	}
	if i != n {
		return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: unexpected character %q in %q", s[i], s)
	}

	scale := len(fracDigits) - exp
	const maxInt32 = int64(1)<<31 - 1
	if int64(scale) > maxInt32 || int64(scale) < -maxInt32-1 {
		return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: scale %d out of range in %q", scale, s)
	}

	un, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return DecFloat{}, fmt.Errorf("h2go: ParseDecFloat: bad digits in %q", s) // unreachable
	}
	if neg {
		un.Neg(un)
	}
	return DecFloat{unscaled: un, scale: int32(scale)}, nil
}

// String renders the exact textual form, byte-identical to
// java.math.BigDecimal.toString() for finite values (and to the special
// spellings otherwise), which is what H2 puts on the wire.
func (d DecFloat) String() string {
	switch d.special {
	case decPosInf:
		return "Infinity"
	case decNegInf:
		return "-Infinity"
	case decNaN:
		return "NaN"
	}

	digits := "0"
	neg := false
	if d.unscaled != nil {
		neg = d.unscaled.Sign() < 0
		digits = new(big.Int).Abs(d.unscaled).String()
	}
	p := len(digits)
	sc := int(d.scale)
	adjusted := p - 1 - sc

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if sc >= 0 && adjusted >= -6 {
		// Plain notation.
		switch {
		case sc == 0:
			b.WriteString(digits)
		case p > sc:
			b.WriteString(digits[:p-sc])
			b.WriteByte('.')
			b.WriteString(digits[p-sc:])
		default:
			b.WriteString("0.")
			b.WriteString(strings.Repeat("0", sc-p))
			b.WriteString(digits)
		}
	} else {
		// Scientific notation with explicit exponent sign.
		b.WriteByte(digits[0])
		if p > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('E')
		if adjusted >= 0 {
			b.WriteByte('+')
		} else {
			b.WriteByte('-')
			adjusted = -adjusted
		}
		b.WriteString(strconv.Itoa(adjusted))
	}
	return b.String()
}

// Float64 converts to the nearest float64. Exactness is not guaranteed for
// values beyond float64 precision; conversion errors are reported (range
// overflows surface as ±Inf with an error, mirroring strconv).
func (d DecFloat) Float64() (float64, error) {
	return strconv.ParseFloat(d.String(), 64)
}

// IsNaN reports whether the value is the DECFLOAT NaN special.
func (d DecFloat) IsNaN() bool { return d.special == decNaN }

// IsInf reports whether the value is +Infinity (sign > 0), −Infinity
// (sign < 0) or either (sign == 0), following math.IsInf conventions.
func (d DecFloat) IsInf(sign int) bool {
	switch {
	case sign > 0:
		return d.special == decPosInf
	case sign < 0:
		return d.special == decNegInf
	default:
		return d.special == decPosInf || d.special == decNegInf
	}
}

// IsFinite reports whether the value is neither infinite nor NaN.
func (d DecFloat) IsFinite() bool { return d.special == decFinite }

// Scale returns the scale (value = unscaled × 10^−scale) of a finite value.
// It is 0 for the zero value and unspecified for specials.
func (d DecFloat) Scale() int32 { return d.scale }

// UnscaledInt returns the unscaled significant digits (including sign) of a
// finite value. The returned pointer aliases internal state and must not be
// mutated. It is nil for the zero value and unspecified for specials.
func (d DecFloat) UnscaledInt() *big.Int { return d.unscaled }

// Scan implements database/sql.Scanner. NULL scans into the zero DecFloat
// (exact 0); string and []byte sources are parsed with ParseDecFloat.
func (d *DecFloat) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = DecFloat{}
		return nil
	case string:
		parsed, err := ParseDecFloat(v)
		if err != nil {
			return fmt.Errorf("h2go: DecFloat.Scan: %w", err)
		}
		*d = parsed
		return nil
	case []byte:
		parsed, err := ParseDecFloat(string(v))
		if err != nil {
			return fmt.Errorf("h2go: DecFloat.Scan: %w", err)
		}
		*d = parsed
		return nil
	default:
		return fmt.Errorf("h2go: DecFloat.Scan: unsupported source type %T", src)
	}
}

// Value implements driver.Valuer: the value is bound as its exact textual
// form. Without prepared-parameter metadata the driver writes it as VARCHAR
// and the server coerces implicitly; with DECFLOAT parameter metadata it is
// written as a typed DECFLOAT wire value.
func (d DecFloat) Value() (driver.Value, error) {
	return d.String(), nil
}
