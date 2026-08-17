package muid

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	benchmarkMuid   Muid
	benchmarkString string

	_ encoding.TextMarshaler     = Muid{}
	_ encoding.TextUnmarshaler   = (*Muid)(nil)
	_ encoding.TextAppender      = Muid{}
	_ encoding.BinaryMarshaler   = Muid{}
	_ encoding.BinaryUnmarshaler = (*Muid)(nil)
	_ sql.Scanner                = (*Muid)(nil)
	_ fmt.Stringer               = Muid{}
	_ driver.Valuer              = Muid{}
)

func TestKnownVectors(t *testing.T) {
	if got, want := (Muid{}).String(), strings.Repeat("0", 19); got != want {
		t.Fatalf("zero String() = %q, want %q", got, want)
	}

	if got := testMuid(0, 1).String(); got[len(got)-1] != '1' {
		t.Fatalf("tail one String() = %q, want final digit 1", got)
	}

	if got, want := testMuid(math.MaxInt64, math.MaxUint32).String(), strings.Repeat("z", 19); got != want {
		t.Fatalf("maximum String() = %q, want %q", got, want)
	}
}

func TestStringReferenceEncoder(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for range 1000 {
		m := testMuid(rng.Int63(), rng.Uint32())
		if got, want := m.String(), referenceString(m); got != want {
			t.Fatalf("String() = %q, reference = %q for %x", got, want, m)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	m := testMuid(1_726_000_000_123_456_789, 0x12345678)

	parsed, err := Parse(m.String())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed != m {
		t.Fatalf("Parse(String()) = %x, want %x", parsed, m)
	}

	binaryData, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	var unmarshaled Muid
	if err := unmarshaled.UnmarshalBinary(binaryData); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}
	if unmarshaled != m {
		t.Fatalf("UnmarshalBinary(MarshalBinary()) = %x, want %x", unmarshaled, m)
	}

	text, err := m.AppendText(nil)
	if err != nil {
		t.Fatalf("AppendText() error = %v", err)
	}
	if got := string(text); got != m.String() {
		t.Fatalf("AppendText() = %q, want %q", got, m.String())
	}
}

func TestNewString(t *testing.T) {
	text := NewString()
	if len(text) != 19 {
		t.Fatalf("NewString() length = %d, want 19", len(text))
	}

	parsed, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(NewString()) error = %v", err)
	}
	if got := parsed.String(); got != text {
		t.Fatalf("Parse(NewString()).String() = %q, want %q", got, text)
	}
}

func TestMustParse(t *testing.T) {
	text := testMuid(123, 456).String()
	want, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := MustParse(text); got != want {
		t.Fatalf("MustParse() = %x, want %x", got, want)
	}

	t.Run("panic", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("MustParse() did not panic for invalid input")
			}
		}()
		MustParse("invalid")
	})
}

func TestTimeAndIsZero(t *testing.T) {
	const ns int64 = 1_726_000_000_123_456_789
	if got, want := testMuid(ns, 0).Time(), time.Unix(0, ns); !got.Equal(want) {
		t.Fatalf("Time() = %v, want %v", got, want)
	}

	if !(Muid{}).IsZero() {
		t.Fatal("zero Muid IsZero() = false, want true")
	}
	if testMuid(1, 0).IsZero() {
		t.Fatal("non-zero Muid IsZero() = true, want false")
	}
}

func TestUnmarshalBinaryRejection(t *testing.T) {
	for _, length := range []int{0, 11, 13} {
		var m Muid
		if err := m.UnmarshalBinary(make([]byte, length)); !errors.Is(err, ErrInvalid) {
			t.Errorf("UnmarshalBinary(%d bytes) error = %v, want error wrapping ErrInvalid", length, err)
		}
	}

	data := make([]byte, 12)
	data[0] = 0x80
	var m Muid
	if err := m.UnmarshalBinary(data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UnmarshalBinary(top-bit-set data) error = %v, want error wrapping ErrInvalid", err)
	}
}

func TestParseCanonicalizationAndRejection(t *testing.T) {
	lower := "000000000000000000a"
	upper := "000000000000000000A"
	lowerMuid, err := Parse(lower)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", lower, err)
	}
	upperMuid, err := Parse(upper)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", upper, err)
	}
	if lowerMuid != upperMuid {
		t.Fatalf("uppercase Parse() = %x, lowercase = %x", upperMuid, lowerMuid)
	}

	one := testMuid(0, 1)
	for _, input := range []string{"i", "I", "l", "L"} {
		parsed, err := Parse(strings.Repeat("0", 18) + input)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
		if parsed != one {
			t.Fatalf("Parse(%q) = %x, want %x", input, parsed, one)
		}
	}

	zero := Muid{}
	for _, input := range []string{"o", "O"} {
		parsed, err := Parse(input + strings.Repeat("0", 18))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
		if parsed != zero {
			t.Fatalf("Parse(%q) = %x, want zero", input, parsed)
		}
	}

	invalidInputs := []string{
		strings.Repeat("0", 18),
		strings.Repeat("0", 20),
		strings.Repeat("0", 18) + "u",
		strings.Repeat("0", 18) + "U",
		strings.Repeat("0", 18) + "!",
		strings.Repeat("0", 18) + string([]byte{0xff}),
		"",
	}
	for _, input := range invalidInputs {
		_, err := Parse(input)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want error wrapping ErrInvalid", input, err)
			continue
		}
		if strings.Contains(err.Error(), "\n") {
			t.Errorf("Parse(%q) error contains a newline: %q", input, err)
		}
	}
}

func TestOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for range 1000 {
		ns1, ns2 := rng.Int63(), rng.Int63()
		m1 := testMuid(ns1, rng.Uint32())
		m2 := testMuid(ns2, rng.Uint32())

		if got, want := sign(m1.Compare(m2)), sign(strings.Compare(m1.String(), m2.String())); got != want {
			t.Fatalf("binary ordering = %d, text ordering = %d for %x and %x", got, want, m1, m2)
		}
		if ns1 != ns2 {
			if got, want := sign(m1.Compare(m2)), sign(compareInt64(ns1, ns2)); got != want {
				t.Fatalf("identifier ordering = %d, timestamp ordering = %d for %d and %d", got, want, ns1, ns2)
			}
		}
	}
}

func TestGeneratorMonotonicity(t *testing.T) {
	var g generator
	previous := g.next(42)
	for range 100 {
		next := g.next(42)
		if next.Compare(previous) <= 0 {
			t.Fatalf("same-time next() = %x, previous = %x", next, previous)
		}
		previous = next
	}

	backward := g.next(1)
	if backward.Compare(previous) <= 0 {
		t.Fatalf("backward-time next() = %x, previous = %x", backward, previous)
	}

	last := int64(123)
	g = generator{last: last, tail: math.MaxUint32}
	before := testMuid(last, math.MaxUint32)
	overflow := g.next(last)
	if got := int64(binary.BigEndian.Uint64(overflow[:8])); got != last+1 {
		t.Fatalf("overflow timestamp = %d, want %d", got, last+1)
	}
	if overflow.Compare(before) <= 0 {
		t.Fatalf("overflow next() = %x, previous = %x", overflow, before)
	}
}

func TestNewConcurrency(t *testing.T) {
	const (
		workers   = 32
		perWorker = 10_000
	)

	values := make([]Muid, workers*perWorker)
	var wg sync.WaitGroup
	for worker := range workers {
		start := worker * perWorker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWorker {
				values[start+i] = New()
			}
		}()
	}
	wg.Wait()

	seen := make(map[Muid]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate muid: %x", value)
		}
		seen[value] = struct{}{}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	want := testMuid(1_726_000_000_123_456_789, 0x87654321)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Muid
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got != want {
		t.Fatalf("JSON round trip = %x, want %x", got, want)
	}
}

func TestValueAndScan(t *testing.T) {
	want := testMuid(1_726_000_000_123_456_789, 0x87654321)
	value, err := want.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("Value() type = %T, want string", value)
	}

	var fromValue Muid
	if err := fromValue.Scan(text); err != nil {
		t.Fatalf("Scan(string) error = %v", err)
	}
	if fromValue != want {
		t.Fatalf("Scan(string) = %x, want %x", fromValue, want)
	}

	binaryData, err := want.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	var fromBinary Muid
	if err := fromBinary.Scan(binaryData); err != nil {
		t.Fatalf("Scan([]byte binary) error = %v", err)
	}
	if fromBinary != want {
		t.Fatalf("Scan([]byte binary) = %x, want %x", fromBinary, want)
	}

	var fromText Muid
	if err := fromText.Scan([]byte(want.String())); err != nil {
		t.Fatalf("Scan([]byte text) error = %v", err)
	}
	if fromText != want {
		t.Fatalf("Scan([]byte text) = %x, want %x", fromText, want)
	}

	var invalidScan Muid
	if err := invalidScan.Scan(1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Scan(int) error = %v, want error wrapping ErrInvalid", err)
	}

	for _, length := range []int{0, 10, 20} {
		var scanned Muid
		if err := scanned.Scan(make([]byte, length)); !errors.Is(err, ErrInvalid) {
			t.Errorf("Scan([]byte of length %d) error = %v, want error wrapping ErrInvalid", length, err)
		}
	}

	topBitSet := make([]byte, 12)
	topBitSet[0] = 0x80
	var scanned Muid
	if err := scanned.Scan(topBitSet); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Scan(top-bit-set binary) error = %v, want error wrapping ErrInvalid", err)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"0000000000000000000",
		"zzzzzzzzzzzzzzzzzzz",
		"000000000000000000I",
		"",
		"000000000000000000u",
		"not-a-valid-muid",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		m, err := Parse(input)
		if err != nil {
			return
		}

		canonical := m.String()
		if len(canonical) != 19 {
			t.Fatalf("String() length = %d, want 19", len(canonical))
		}
		parsed, err := Parse(canonical)
		if err != nil {
			t.Fatalf("Parse(String()) error = %v", err)
		}
		if parsed != m {
			t.Fatalf("Parse(String()) = %x, want %x", parsed, m)
		}
	})
}

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		benchmarkMuid = New()
	}
}

func BenchmarkNewParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			New()
		}
	})
}

func BenchmarkString(b *testing.B) {
	m := testMuid(1_726_000_000_123_456_789, 0x12345678)
	b.ReportAllocs()
	for range b.N {
		benchmarkString = m.String()
	}
}

func BenchmarkParse(b *testing.B) {
	text := testMuid(1_726_000_000_123_456_789, 0x12345678).String()
	b.ReportAllocs()
	for range b.N {
		var err error
		benchmarkMuid, err = Parse(text)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func testMuid(ns int64, tail uint32) Muid {
	var m Muid
	binary.BigEndian.PutUint64(m[:8], uint64(ns))
	binary.BigEndian.PutUint32(m[8:], tail)
	return m
}

func referenceString(m Muid) string {
	const referenceAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

	value := new(big.Int).SetBytes(m[:])
	base := big.NewInt(32)
	quotient := new(big.Int)
	remainder := new(big.Int)
	var text [19]byte
	for i := len(text) - 1; i >= 0; i-- {
		quotient.QuoRem(value, base, remainder)
		text[i] = referenceAlphabet[remainder.Int64()]
		value.Set(quotient)
	}
	return string(text[:])
}

func sign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
