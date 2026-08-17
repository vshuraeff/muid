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
	benchmarkMu     sync.Mutex

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
	for _, vector := range []struct {
		name     string
		ts       uint64
		rnd      uint16
		wantHex  string
		wantText string
	}{
		{
			name:     "zero timestamp",
			ts:       0,
			rnd:      0,
			wantHex:  "00000000000000000000e139",
			wantText: "0000000000000Ezx",
		},
		{
			name:     "previous upper timestamp",
			ts:       math.MaxInt64,
			rnd:      math.MaxUint16,
			wantHex:  "7fffffffffffffffffff42d5",
			wantText: "pWE94k9hlxnYxc3R",
		},
		{
			name:     "top-bit timestamp",
			ts:       uint64(1) << 63,
			rnd:      0,
			wantHex:  "80000000000000000000050d",
			wantText: "pWE94k9hlxnYxozN",
		},
		{
			name:     "maximum timestamp with rnd=0xffff",
			ts:       11_099_595_973_925_556_392,
			rnd:      math.MaxUint16,
			wantHex:  "9a09afbae83050a8ffff6f0a",
			wantText: "zzzzzzzzzzvvvln8",
		},
	} {
		t.Run(vector.name, func(t *testing.T) {
			want := referenceMuid(vector.ts, vector.rnd)
			got := newMuid(vector.ts, vector.rnd)
			if gotHex := fmt.Sprintf("%x", got[:]); gotHex != vector.wantHex {
				t.Fatalf("newMuid() hex = %q, want %q", gotHex, vector.wantHex)
			}
			if gotText := got.String(); gotText != vector.wantText {
				t.Fatalf("String() = %q, want %q", gotText, vector.wantText)
			}
			if got != want {
				t.Fatalf("newMuid() = %x, want %x", got, want)
			}
			if gotCRC, wantCRC := binary.BigEndian.Uint16(got[10:12]), referenceCRC16(got[:10]); gotCRC != wantCRC {
				t.Fatalf("stored CRC = %04x, reference CRC = %04x", gotCRC, wantCRC)
			}
			if gotText, wantText := got.String(), referenceString(want); gotText != wantText {
				t.Fatalf("String() = %q, reference = %q", gotText, wantText)
			}
			parsed, err := Parse(vector.wantText)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", vector.wantText, err)
			}
			if parsed != got {
				t.Fatalf("Parse(%q) = %x, want %x", vector.wantText, parsed, got)
			}
		})
	}
}

func TestMaxValidPlusOne(t *testing.T) {
	bound := new(big.Int).Exp(big.NewInt(62), big.NewInt(textLength), nil)
	if bound.BitLen() > 96 {
		t.Fatalf("62^16 bit length = %d, want at most 96", bound.BitLen())
	}

	var want [12]byte
	bound.FillBytes(want[:])
	if maxValidPlusOne != want {
		t.Fatalf("maxValidPlusOne = %x, want %x", maxValidPlusOne, want)
	}
}

func TestMaximumTimestampWithMaxRandom(t *testing.T) {
	const wantTimestamp uint64 = 11_099_595_973_925_556_392

	bound := new(big.Int).Exp(big.NewInt(62), big.NewInt(textLength), nil)
	limit := new(big.Int).Sub(bound, big.NewInt(1))
	randomPart := new(big.Int).Lsh(new(big.Int).SetUint64(math.MaxUint16), 16)
	candidate := limit.Sub(limit, randomPart)
	candidate.Rsh(candidate, 32)

	for {
		if candidate.BitLen() > 64 {
			t.Fatalf("candidate timestamp has %d bits, want at most 64", candidate.BitLen())
		}
		fixture := referenceMuid(candidate.Uint64(), math.MaxUint16)
		if new(big.Int).SetBytes(fixture[:]).Cmp(bound) < 0 {
			ts := candidate.Uint64()
			if ts != wantTimestamp {
				t.Fatalf("maximum timestamp with rnd=0xffff = %d, want %d", ts, wantTimestamp)
			}

			got := newMuid(ts, math.MaxUint16)
			if got != fixture {
				t.Fatalf("newMuid() = %x, want %x", got, fixture)
			}
			if gotText, wantText := got.String(), referenceString(fixture); gotText != wantText {
				t.Fatalf("String() = %q, reference = %q", gotText, wantText)
			}
			parsed, err := Parse(got.String())
			if err != nil {
				t.Fatalf("Parse(String()) error = %v", err)
			}
			if parsed != got {
				t.Fatalf("Parse(String()) = %x, want %x", parsed, got)
			}
			var fromBinary Muid
			if err := fromBinary.UnmarshalBinary(got[:]); err != nil {
				t.Fatalf("UnmarshalBinary() error = %v", err)
			}
			if fromBinary != got {
				t.Fatalf("UnmarshalBinary() = %x, want %x", fromBinary, got)
			}
			var fromScan Muid
			if err := fromScan.Scan(got[:]); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if fromScan != got {
				t.Fatalf("Scan() = %x, want %x", fromScan, got)
			}

			next := newMuid(ts+1, math.MaxUint16)
			if new(big.Int).SetBytes(next[:]).Cmp(bound) < 0 {
				t.Fatalf("next timestamp value = %x, want at least 62^16", next)
			}
			if err := fromBinary.UnmarshalBinary(next[:]); !errors.Is(err, ErrInvalid) {
				t.Fatalf("UnmarshalBinary(next timestamp) error = %v, want ErrInvalid", err)
			}
			return
		}
		candidate.Sub(candidate, big.NewInt(1))
	}
}

func TestCRCCheckValue(t *testing.T) {
	const want uint16 = 0x29b1
	input := []byte("123456789")
	if got := crc16(input); got != want {
		t.Fatalf("crc16() = %04x, want %04x", got, want)
	}
	if got := referenceCRC16(input); got != want {
		t.Fatalf("referenceCRC16() = %04x, want %04x", got, want)
	}
}

func TestReferenceAlgorithms(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for range 1000 {
		ts := uint64(rng.Int63())
		rnd := uint16(rng.Uint32())
		fixture := referenceMuid(ts, rnd)
		production := newMuid(ts, rnd)
		if production != fixture {
			t.Fatalf("newMuid(%d, %04x) = %x, want %x", ts, rnd, production, fixture)
		}
		if gotCRC, wantCRC := binary.BigEndian.Uint16(production[10:12]), referenceCRC16(fixture[:10]); gotCRC != wantCRC {
			t.Fatalf("stored CRC = %04x, reference CRC = %04x", gotCRC, wantCRC)
		}
		if gotText, wantText := production.String(), referenceString(fixture); gotText != wantText {
			t.Fatalf("String() = %q, reference = %q for %x", gotText, wantText, fixture)
		}
		parsed, err := Parse(referenceString(fixture))
		if err != nil {
			t.Fatalf("Parse(reference string) error = %v", err)
		}
		if parsed != fixture {
			t.Fatalf("Parse(reference string) = %x, want %x", parsed, fixture)
		}
	}
}

func TestRoundTrips(t *testing.T) {
	want := newMuid(1_726_000_000_123_456_789, 0x8765)

	parsed, err := Parse(want.String())
	if err != nil {
		t.Fatalf("Parse(String()) error = %v", err)
	}
	if parsed != want || parsed.String() != want.String() {
		t.Fatalf("Parse(String()) = %x / %q, want %x / %q", parsed, parsed.String(), want, want.String())
	}

	text, err := want.AppendText([]byte("prefix:"))
	if err != nil {
		t.Fatalf("AppendText() error = %v", err)
	}
	if got, wantText := string(text), "prefix:"+want.String(); got != wantText {
		t.Fatalf("AppendText() = %q, want %q", got, wantText)
	}

	binaryData, err := want.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	var fromBinary Muid
	if err := fromBinary.UnmarshalBinary(binaryData); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}
	if fromBinary != want {
		t.Fatalf("UnmarshalBinary(MarshalBinary()) = %x, want %x", fromBinary, want)
	}

	jsonData, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var fromJSON Muid
	if err := json.Unmarshal(jsonData, &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if fromJSON != want {
		t.Fatalf("JSON round trip = %x, want %x", fromJSON, want)
	}

	value, err := want.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	var fromValue Muid
	if err := fromValue.Scan(value); err != nil {
		t.Fatalf("Scan(Value()) error = %v", err)
	}
	if fromValue != want {
		t.Fatalf("Scan(Value()) = %x, want %x", fromValue, want)
	}

	var fromTextBytes Muid
	if err := fromTextBytes.Scan([]byte(want.String())); err != nil {
		t.Fatalf("Scan([]byte text) error = %v", err)
	}
	if fromTextBytes != want {
		t.Fatalf("Scan([]byte text) = %x, want %x", fromTextBytes, want)
	}

	var fromRaw Muid
	if err := fromRaw.Scan(binaryData); err != nil {
		t.Fatalf("Scan([]byte binary) error = %v", err)
	}
	if fromRaw != want {
		t.Fatalf("Scan([]byte binary) = %x, want %x", fromRaw, want)
	}
}

func TestReceiverUnchangedOnFailure(t *testing.T) {
	original := newMuid(1_726_000_000_123_456_789, 0x5678)
	badText := strings.Repeat("0", textLength)
	badBinary := original
	badBinary[11] ^= 1

	t.Run("UnmarshalText", func(t *testing.T) {
		receiver := original
		if err := receiver.UnmarshalText([]byte(badText)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("UnmarshalText() error = %v, want ErrInvalid", err)
		}
		if receiver != original {
			t.Fatalf("receiver = %x after failed UnmarshalText(), want %x", receiver, original)
		}
	})

	t.Run("UnmarshalBinary", func(t *testing.T) {
		receiver := original
		if err := receiver.UnmarshalBinary(badBinary[:]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("UnmarshalBinary() error = %v, want ErrInvalid", err)
		}
		if receiver != original {
			t.Fatalf("receiver = %x after failed UnmarshalBinary(), want %x", receiver, original)
		}
	})

	t.Run("Scan", func(t *testing.T) {
		receiver := original
		if err := receiver.Scan(badText); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Scan(string) error = %v, want ErrInvalid", err)
		}
		if receiver != original {
			t.Fatalf("receiver = %x after failed Scan(), want %x", receiver, original)
		}
	})
}

func TestNilReceivers(t *testing.T) {
	var receiver *Muid
	if err := receiver.UnmarshalText([]byte("invalid")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil UnmarshalText() error = %v, want ErrInvalid", err)
	}
	if err := receiver.UnmarshalBinary(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil UnmarshalBinary() error = %v, want ErrInvalid", err)
	}
	if err := receiver.Scan("invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil Scan() error = %v, want ErrInvalid", err)
	}
}

func TestNewStringAndMustParse(t *testing.T) {
	text := NewString()
	if len(text) != textLength {
		t.Fatalf("NewString() length = %d, want %d", len(text), textLength)
	}
	parsed, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(NewString()) error = %v", err)
	}
	if parsed.String() != text {
		t.Fatalf("Parse(NewString()).String() = %q, want %q", parsed.String(), text)
	}
	if got := MustParse(text); got != parsed {
		t.Fatalf("MustParse() = %x, want %x", got, parsed)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MustParse() did not panic for invalid input")
		}
	}()
	MustParse("invalid")
}

func TestTimeAndIsZero(t *testing.T) {
	const ns uint64 = 1_726_000_000_123_456_789
	if got, want := newMuid(ns, 0).Time(), time.Unix(int64(ns/1_000_000_000), int64(ns%1_000_000_000)); !got.Equal(want) {
		t.Fatalf("Time() = %v, want %v", got, want)
	}

	t.Run("above MaxInt64", func(t *testing.T) {
		ts := uint64(1<<63) + 12_345
		want := time.Unix(int64(ts/1_000_000_000), int64(ts%1_000_000_000)).UTC()
		if got := newMuid(ts, 0).Time().UTC(); !got.Equal(want) {
			t.Fatalf("Time() = %v, want %v", got, want)
		}
	})
	if !(Muid{}).IsZero() {
		t.Fatal("zero Muid IsZero() = false, want true")
	}
	if newMuid(1, 0).IsZero() {
		t.Fatal("non-zero Muid IsZero() = true, want false")
	}
}

func TestParseRejection(t *testing.T) {
	valid := newMuid(1_726_000_000_123_456_789, 0x1234).String()
	for _, input := range []string{"", valid[:15], valid + "0"} {
		assertInvalid(t, input, "wrong length")
	}

	for _, replacement := range []string{" ", "-", string([]byte{0xff}), string([]byte{0})} {
		input := valid[:7] + replacement + valid[8:]
		assertInvalid(t, input, "invalid character")
	}

	assertChecksumMismatch(t, strings.Repeat("0", textLength))
	assertChecksumMismatch(t, strings.Repeat("z", textLength))

	corrupt := newMuid(9876, 0xabcd)
	corrupt[10] ^= 1
	assertChecksumMismatch(t, corrupt.String())
}

func TestCaseCorruptionIsNotAnAlias(t *testing.T) {
	original := newMuid(math.MaxInt64, math.MaxUint16)
	input, changed := flipLetterCase(original.String())
	if !changed {
		t.Fatal("test identifier has no letter to flip")
	}

	parsed, err := Parse(input)
	if err != nil {
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Parse(case-corrupted input) error = %v, want ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("Parse(case-corrupted input) error = %v, want checksum mismatch", err)
		}
		return
	}
	if parsed == original {
		t.Fatalf("Parse(%q) accepted an equivalent value", input)
	}
}

func TestUnmarshalBinaryAndScanRejection(t *testing.T) {
	for _, length := range []int{0, 11, 13} {
		var m Muid
		if err := m.UnmarshalBinary(make([]byte, length)); !errors.Is(err, ErrInvalid) {
			t.Errorf("UnmarshalBinary(%d bytes) error = %v, want ErrInvalid", length, err)
		}
	}

	valid := newMuid(123, 456)
	var m Muid
	bound := maxValidPlusOne
	allOnes := Muid{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for name, data := range map[string][]byte{
		"bound":    bound[:],
		"all ones": allOnes[:],
	} {
		if err := m.UnmarshalBinary(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("UnmarshalBinary(%s) error = %v, want ErrInvalid", name, err)
		}
		if err := m.Scan(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("Scan(%s) error = %v, want ErrInvalid", name, err)
		}
	}
	badCRC := valid
	badCRC[11] ^= 1
	if err := m.UnmarshalBinary(badCRC[:]); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("UnmarshalBinary(bad checksum) error = %v, want checksum ErrInvalid", err)
	}

	for _, value := range []any{1, []byte{}, make([]byte, 13)} {
		var scanned Muid
		if err := scanned.Scan(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("Scan(%T) error = %v, want ErrInvalid", value, err)
		}
	}
	var scanned Muid
	if err := scanned.Scan(badCRC[:]); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Scan(bad checksum) error = %v, want checksum ErrInvalid", err)
	}
	var textScanned Muid
	if err := textScanned.Scan([]byte("0000000000000000")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Scan(16-byte text, bad checksum) error = %v, want ErrInvalid", err)
	}
}

func TestOrdering(t *testing.T) {
	equal := newMuid(123, 456)
	if got := equal.Compare(equal); got != 0 {
		t.Fatalf("Compare(equal) = %d, want 0", got)
	}

	rng := rand.New(rand.NewSource(2))
	for range 1000 {
		ts1, ts2 := uint64(rng.Int63()), uint64(rng.Int63())
		if ts1 == ts2 {
			ts2++
		}
		m1 := referenceMuid(ts1, uint16(rng.Uint32()))
		m2 := referenceMuid(ts2, uint16(rng.Uint32()))

		if got, want := sign(m1.Compare(m2)), sign(strings.Compare(m1.String(), m2.String())); got != want {
			t.Fatalf("binary ordering = %d, text ordering = %d for %x and %x", got, want, m1, m2)
		}
		if got, want := sign(m1.Compare(m2)), sign(compareUint64(ts1, ts2)); got != want {
			t.Fatalf("identifier ordering = %d, timestamp ordering = %d for %d and %d", got, want, ts1, ts2)
		}
	}
}

func TestGeneratorMonotonicity(t *testing.T) {
	var g generator
	previous := g.next(42)
	for range 100 {
		next := g.next(42)
		if next.Compare(previous) <= 0 || strings.Compare(next.String(), previous.String()) <= 0 {
			t.Fatalf("same-time next() = %x / %q, previous = %x / %q", next, next.String(), previous, previous.String())
		}
		previous = next
	}

	backward := g.next(1)
	if backward.Compare(previous) <= 0 || strings.Compare(backward.String(), previous.String()) <= 0 {
		t.Fatalf("backward-time next() = %x / %q, previous = %x / %q", backward, backward.String(), previous, previous.String())
	}

	last := uint64(123)
	g = generator{last: last, rnd: math.MaxUint16}
	before := newMuid(last, math.MaxUint16)
	overflow := g.next(last)
	if got := binary.BigEndian.Uint64(overflow[:8]); got != last+1 {
		t.Fatalf("overflow timestamp = %d, want %d", got, last+1)
	}
	if g.last != last+1 {
		t.Fatalf("generator last = %d, want %d", g.last, last+1)
	}
	if overflow.Compare(before) <= 0 || strings.Compare(overflow.String(), before.String()) <= 0 {
		t.Fatalf("overflow next() = %x / %q, previous = %x / %q", overflow, overflow.String(), before, before.String())
	}
}

func TestGeneratorClampedNegativeTimeRecovery(t *testing.T) {
	for _, test := range []struct {
		name string
		in   int64
		want uint64
	}{
		{name: "minimum", in: math.MinInt64, want: 0},
		{name: "negative", in: -1, want: 0},
		{name: "zero", in: 0, want: 0},
		{name: "positive", in: 1, want: 1},
		{name: "maximum", in: math.MaxInt64, want: uint64(math.MaxInt64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := clampNanos(test.in); got != test.want {
				t.Fatalf("clampNanos(%d) = %d, want %d", test.in, got, test.want)
			}
		})
	}

	var g generator
	clamped := g.next(0)
	if got := binary.BigEndian.Uint64(clamped[:8]); got != 0 {
		t.Fatalf("clamped timestamp = %d, want 0", got)
	}
	parsed, err := Parse(clamped.String())
	if err != nil {
		t.Fatalf("Parse(clamped.String()) error = %v", err)
	}
	if parsed != clamped {
		t.Fatalf("Parse(clamped.String()) = %x, want %x", parsed, clamped)
	}
	var fromBinary Muid
	if err := fromBinary.UnmarshalBinary(clamped[:]); err != nil {
		t.Fatalf("UnmarshalBinary(clamped) error = %v", err)
	}
	if fromBinary != clamped {
		t.Fatalf("UnmarshalBinary(clamped) = %x, want %x", fromBinary, clamped)
	}

	const now uint64 = 123
	recovered := g.next(now)
	if got := binary.BigEndian.Uint64(recovered[:8]); got != now {
		t.Fatalf("recovered timestamp = %d, want %d", got, now)
	}
	if g.last != now {
		t.Fatalf("generator last = %d, want %d", g.last, now)
	}
	if recovered.Compare(clamped) <= 0 {
		t.Fatalf("recovered muid = %x, want greater than clamped muid %x", recovered, clamped)
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

func FuzzParse(f *testing.F) {
	f.Add(newMuid(1_726_000_000_123_456_789, 0x1234).String())
	f.Add(strings.Repeat("0", textLength))
	f.Add(strings.Repeat("z", textLength))
	f.Add("not-a-valid-muid")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		m, err := Parse(input)
		if err != nil {
			return
		}
		if got := m.String(); got != input {
			t.Fatalf("String() = %q, want accepted input %q", got, input)
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
		var result Muid
		for pb.Next() {
			result = New()
		}
		benchmarkMu.Lock()
		benchmarkMuid = result
		benchmarkMu.Unlock()
	})
}

func BenchmarkString(b *testing.B) {
	m := newMuid(1_726_000_000_123_456_789, 0x1234)
	b.ReportAllocs()
	for range b.N {
		benchmarkString = m.String()
	}
}

func BenchmarkParse(b *testing.B) {
	text := newMuid(1_726_000_000_123_456_789, 0x1234).String()
	b.ReportAllocs()
	for range b.N {
		var err error
		benchmarkMuid, err = Parse(text)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func referenceMuid(ns uint64, rnd uint16) Muid {
	var m Muid
	binary.BigEndian.PutUint64(m[:8], ns)
	binary.BigEndian.PutUint16(m[8:10], rnd)
	binary.BigEndian.PutUint16(m[10:12], referenceCRC16(m[:10]))
	return m
}

func referenceCRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func referenceString(m Muid) string {
	const referenceAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	value := new(big.Int).SetBytes(m[:])
	base := big.NewInt(62)
	quotient := new(big.Int)
	remainder := new(big.Int)
	var text [textLength]byte
	for i := len(text) - 1; i >= 0; i-- {
		quotient.QuoRem(value, base, remainder)
		text[i] = referenceAlphabet[remainder.Int64()]
		value.Set(quotient)
	}
	return string(text[:])
}

func assertInvalid(t *testing.T, input, name string) {
	t.Helper()
	if _, err := Parse(input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse(%s) error = %v, want ErrInvalid", name, err)
	}
}

func assertChecksumMismatch(t *testing.T, input string) {
	t.Helper()
	_, err := Parse(input)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Parse(%q) error = %v, want checksum ErrInvalid", input, err)
	}
}

func flipLetterCase(input string) (string, bool) {
	for i := range input {
		c := input[i]
		switch {
		case c >= 'A' && c <= 'Z':
			return input[:i] + string(c-'A'+'a') + input[i+1:], true
		case c >= 'a' && c <= 'z':
			return input[:i] + string(c-'a'+'A') + input[i+1:], true
		}
	}
	return input, false
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

func compareUint64(left, right uint64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
