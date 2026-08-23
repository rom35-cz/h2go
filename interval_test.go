// interval_test.go — golden-string tests for canonical INTERVAL text
// formatting (docs/internal/MATURITY_ROUND_II_PLAN.md Task 2, finding 3).
//
// Every golden string was live-verified against H2 2.4.240 via
// SELECT CAST(<expression> AS VARCHAR) on 2026-08-22 (see the matrix in
// docs/internal/MATURITY_ROUND_II_PLAN.md Task 2 and TestIntegration_IntervalCanonicalMatrix
// for the permanent live check).

package h2go

import (
	"bytes"
	"testing"
	"time"
)

func ns(d time.Duration) int64 { return int64(d) }

// TestFormatIntervalCanonicalMatrix pins formatInterval to H2's canonical
// output for every qualifier.
func TestFormatIntervalCanonicalMatrix(t *testing.T) {
	hmsHalf := ns(2*time.Hour + 3*time.Minute + 4*time.Second + 500*time.Millisecond)
	tests := []struct {
		name      string
		qualifier string
		leading   int64
		remaining int64
		want      string
	}{
		{"SECOND fractional", "SECOND", 5, ns(250 * time.Millisecond), "INTERVAL '5.25' SECOND"},
		{"SECOND trailing zeros trimmed", "SECOND", 7, ns(750 * time.Millisecond), "INTERVAL '7.75' SECOND"},
		{"SECOND integral no fraction", "SECOND", 7, 0, "INTERVAL '7' SECOND"},
		{"SECOND zero leading", "SECOND", 0, ns(500 * time.Millisecond), "INTERVAL '0.5' SECOND"},
		{"SECOND negative", "SECOND", -5, ns(250 * time.Millisecond), "INTERVAL '-5.25' SECOND"},
		{"SECOND nine digits", "SECOND", 59, 999999999, "INTERVAL '59.999999999' SECOND"},

		{"DAY TO SECOND plain", "DAY TO SECOND", 1, hmsHalf, "INTERVAL '1 02:03:04.5' DAY TO SECOND"},
		{"DAY TO SECOND one nano padded", "DAY TO SECOND", 1, hmsHalf - ns(500*time.Millisecond) + 1,
			"INTERVAL '1 02:03:04.000000001' DAY TO SECOND"},
		{"DAY TO SECOND zero fraction dropped", "DAY TO SECOND", 1, hmsHalf - ns(500*time.Millisecond),
			"INTERVAL '1 02:03:04' DAY TO SECOND"},
		{"DAY TO SECOND all zero", "DAY TO SECOND", 0, 0, "INTERVAL '0 00:00:00' DAY TO SECOND"},
		{"DAY TO SECOND negative", "DAY TO SECOND", -1, hmsHalf, "INTERVAL '-1 02:03:04.5' DAY TO SECOND"},
		{"DAY TO SECOND zero day kept", "DAY TO SECOND", 0, hmsHalf, "INTERVAL '0 02:03:04.5' DAY TO SECOND"},

		{"HOUR TO SECOND max", "HOUR TO SECOND", 23, ns(59*time.Minute + 59*time.Second + 999999999),
			"INTERVAL '23:59:59.999999999' HOUR TO SECOND"},
		{"HOUR TO SECOND plain", "HOUR TO SECOND", 2, ns(3*time.Minute + 4*time.Second + 500*time.Millisecond),
			"INTERVAL '2:03:04.5' HOUR TO SECOND"},
		{"HOUR TO SECOND negative", "HOUR TO SECOND", -2, ns(3*time.Minute + 4*time.Second + 500*time.Millisecond),
			"INTERVAL '-2:03:04.5' HOUR TO SECOND"},
		{"HOUR TO SECOND zero hour", "HOUR TO SECOND", 0, ns(3*time.Minute + 4*time.Second),
			"INTERVAL '0:03:04' HOUR TO SECOND"},
		{"HOUR TO SECOND large hours unpadded", "HOUR TO SECOND", 100, ns(3*time.Minute + 4*time.Second + 567890123),
			"INTERVAL '100:03:04.567890123' HOUR TO SECOND"},

		{"MINUTE TO SECOND plain", "MINUTE TO SECOND", 3, ns(4*time.Second + 500*time.Millisecond),
			"INTERVAL '3:04.5' MINUTE TO SECOND"},
		{"MINUTE TO SECOND negative", "MINUTE TO SECOND", -3, ns(4*time.Second + 500*time.Millisecond),
			"INTERVAL '-3:04.5' MINUTE TO SECOND"},
		{"MINUTE TO SECOND zero minute", "MINUTE TO SECOND", 0, 999999999,
			"INTERVAL '0:00.999999999' MINUTE TO SECOND"},

		{"DAY TO HOUR pads hours", "DAY TO HOUR", 2, 3, "INTERVAL '2 03' DAY TO HOUR"},
		{"DAY TO HOUR negative", "DAY TO HOUR", -2, 3, "INTERVAL '-2 03' DAY TO HOUR"},

		{"DAY TO MINUTE pads both", "DAY TO MINUTE", 2, 3*60 + 5, "INTERVAL '2 03:05' DAY TO MINUTE"},
		{"DAY TO MINUTE negative", "DAY TO MINUTE", -2, 3*60 + 5, "INTERVAL '-2 03:05' DAY TO MINUTE"},

		{"HOUR TO MINUTE hours unpadded", "HOUR TO MINUTE", 2, 3, "INTERVAL '2:03' HOUR TO MINUTE"},
		{"HOUR TO MINUTE negative", "HOUR TO MINUTE", -12, 5, "INTERVAL '-12:05' HOUR TO MINUTE"},

		{"YEAR TO MONTH plain", "YEAR TO MONTH", 1, 6, "INTERVAL '1-6' YEAR TO MONTH"},
		{"YEAR TO MONTH zero months", "YEAR TO MONTH", 1, 0, "INTERVAL '1-0' YEAR TO MONTH"},
		{"YEAR TO MONTH zero years", "YEAR TO MONTH", 0, 1, "INTERVAL '0-1' YEAR TO MONTH"},
		{"YEAR TO MONTH negative", "YEAR TO MONTH", -1, 6, "INTERVAL '-1-6' YEAR TO MONTH"},

		{"single DAY", "DAY", 42, 0, "INTERVAL '42' DAY"},
		{"single MONTH negative", "MONTH", -7, 0, "INTERVAL '-7' MONTH"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatInterval(tc.qualifier, tc.leading, tc.remaining)
			if got != tc.want {
				t.Errorf("formatInterval(%q, %d, %d) = %q, want %q",
					tc.qualifier, tc.leading, tc.remaining, got, tc.want)
			}
		})
	}
}

// TestReadIntervalValueWireSigns exercises the stream decoding path with
// synthetic frames, including the negated qualifier byte used by H2 for
// negative intervals. The wire carries leading/remaining as unsigned
// magnitudes; readIntervalValue folds the sign into leading.
func TestReadIntervalValueWireSigns(t *testing.T) {
	negOrdinal := func(ordinal byte) byte { return ^ordinal }
	ordinalOf := func(wire byte) int {
		if wire >= 128 {
			return int(^wire)
		}
		return int(wire)
	}

	tests := []struct {
		name      string
		ordinal   byte
		leading   int64
		remaining int64
		want      string
	}{
		{"positive SECOND", 5, 5, ns(250 * time.Millisecond), "INTERVAL '5.25' SECOND"},
		{"negative SECOND", negOrdinal(5), 5, ns(250 * time.Millisecond), "INTERVAL '-5.25' SECOND"},
		{"negative YEAR TO MONTH", negOrdinal(6), 1, 6, "INTERVAL '-1-6' YEAR TO MONTH"},
		{"negative DAY TO SECOND", negOrdinal(9), 1, hnsForDTS(),
			"INTERVAL '-1 02:03:04.5' DAY TO SECOND"},
		{"positive DAY TO HOUR", 7, 2, 3, "INTERVAL '2 03' DAY TO HOUR"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			writeValueType(buf, ValueTypeInterval)
			buf.WriteByte(tc.ordinal)
			writeInt64(buf, tc.leading)
			if ordinalOf(tc.ordinal) >= 5 {
				writeInt64(buf, tc.remaining)
			}

			tr := mockTransferFromBytes(buf.Bytes())
			got, err := tr.ReadValue(nil)
			if err != nil {
				t.Fatalf("ReadValue: %v", err)
			}
			if got != tc.want {
				t.Errorf("decoded = %v, want %v", got, tc.want)
			}
		})
	}
}

// hnsForDTS returns the remaining-nanos value of 02:03:04.5 for a
// DAY TO SECOND frame.
func hnsForDTS() int64 {
	return ns(2*time.Hour + 3*time.Minute + 4*time.Second + 500*time.Millisecond)
}
