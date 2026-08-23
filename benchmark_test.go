// benchmark_test.go — pure (server-free) benchmarks for the driver hot paths:
// DSN parsing, wire primitives, UTF-16 codec, value encode/decode, interval
// formatting and DECFLOAT parsing. Run with:
//
//	go test -bench . -benchmem ./...
//
// Live-server benchmarks live in benchmark_integration_test.go.

package h2go

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// loopReader serves data cyclically, forever: an infinite supply of
// self-delimiting frames so read benchmarks never hit EOF. The data must
// consist of whole frames; cycling at len(data) preserves frame alignment.
type loopReader struct {
	data []byte
	off  int
}

func (l *loopReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		copied := copy(p[n:], l.data[l.off:])
		l.off += copied
		n += copied
		if l.off == len(l.data) {
			l.off = 0
		}
		if copied == 0 {
			break
		}
	}
	return n, nil
}

var _ io.Reader = (*loopReader)(nil)

func newBenchTrRead(data []byte) *Tr {
	return NewReader(&loopReader{data: data})
}

func BenchmarkParseDSN(b *testing.B) {
	const dsn = "jdbc:h2:tcp://localhost:9092/mydb;USER=app;PASSWORD=secret"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseDSN(dsn); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTrWriteStringASCII(b *testing.B) {
	tr := NewWriter(nopWriteCloser{io.Discard})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := tr.WriteString("SELECT id, name FROM people WHERE id = ?"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTrWriteStringNonBMP(b *testing.B) {
	tr := NewWriter(nopWriteCloser{io.Discard})
	s := "emoji \U0001F600 and čeština text"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := tr.WriteString(s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTrReadStringASCII(b *testing.B) {
	frame := wireStringFrame("SELECT id, name FROM people WHERE id = ?")
	blob := bytes.Repeat(frame, 64)
	tr := newBenchTrRead(blob)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tr.ReadString(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTrReadStringNonBMP(b *testing.B) {
	frame := wireStringFrame("emoji \U0001F600 and čeština text")
	blob := bytes.Repeat(frame, 64)
	tr := newBenchTrRead(blob)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tr.ReadString(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueWriteInt64(b *testing.B) {
	tr := NewWriter(nopWriteCloser{io.Discard})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := tr.WriteValue(int64(i), nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueWriteString(b *testing.B) {
	tr := NewWriter(nopWriteCloser{io.Discard})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := tr.WriteValue("row-123456", nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueReadInt64(b *testing.B) {
	var buf bytes.Buffer
	w := NewWriter(nopWriteCloser{&buf})
	_ = w.WriteValue(int64(123456789), nil)
	_ = w.Flush()
	blob := bytes.Repeat(buf.Bytes(), 64)
	tr := newBenchTrRead(blob)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tr.ReadValue(nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueReadVarchar(b *testing.B) {
	var buf bytes.Buffer
	w := NewWriter(nopWriteCloser{&buf})
	_ = w.WriteValue("row-123456", nil)
	_ = w.Flush()
	blob := bytes.Repeat(buf.Bytes(), 64)
	tr := newBenchTrRead(blob)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tr.ReadValue(nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormatInterval(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = formatInterval("DAY TO SECOND", 1, 24*3600*intervalNanosPerSecond+2*intervalNanosPerMinute+3*intervalNanosPerSecond+500000000)
	}
}

func BenchmarkParseDecFloat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseDecFloat("123456789012345678901234567890.42"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecFloatString(b *testing.B) {
	df, _ := ParseDecFloat("1.23456789012345678901234567890123456789E+25")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = df.String()
	}
}

// ---- helpers ----

type nopWriteCloser struct{ w io.Writer }

func (n nopWriteCloser) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n nopWriteCloser) Close() error                { return nil }
func (n nopWriteCloser) SetDeadline(time.Time) error { return nil }
func (n nopWriteCloser) Read(_ []byte) (int, error)  { return 0, io.EOF }

var _ deadlineSetterReadWriteCloser = nopWriteCloser{}

// wireStringFrame encodes one H2 wire string (int32 length + big-endian
// UTF-16 code units).
func wireStringFrame(s string) []byte {
	var out bytes.Buffer
	w := NewWriter(nopWriteCloser{&out})
	if err := w.WriteString(s); err != nil {
		panic(err)
	}
	if err := w.Flush(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

var _ = context.Background // keep context import if helpers change
