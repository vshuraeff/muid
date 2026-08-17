// Package muid implements a 96-bit monotonic, sortable opaque identifier: uniqueness is strict per process and cross-process collisions have about a 2^-32 chance for a pair generated in the same nanosecond. Its binary and text forms sort in generation order. It is not a security token: tails within one nanosecond are sequential rather than uniformly random, so muid must not be used as an unguessable secret. Nanoseconds are the storage unit, not a guaranteed clock resolution; wall-clock corrections can affect cross-process ordering. The 12-byte big-endian format consists of an int64 Unix-nanoseconds timestamp whose top bit is always zero, followed by a uint32 tail; its text form is exactly 19 characters of lowercase Crockford base32. The timestamp range extends through roughly the year 2262, matching time.Time.UnixNano; beyond that, the top-bit-zero invariant and monotonic ordering assumptions no longer hold.
package muid

import (
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	alphabet     = "0123456789abcdefghjkmnpqrstvwxyz"
	invalidDigit = 0xff
)

var decodeTable = func() [256]byte {
	var table [256]byte
	for i := range table {
		table[i] = invalidDigit
	}
	for i := range alphabet {
		c := alphabet[i]
		table[c] = byte(i)
		if c >= 'a' && c <= 'z' {
			table[c-'a'+'A'] = byte(i)
		}
	}
	table['i'], table['I'] = 1, 1
	table['l'], table['L'] = 1, 1
	table['o'], table['O'] = 0, 0
	return table
}()

// ErrInvalid reports invalid muid text, binary data, or database input.
var ErrInvalid = errors.New("invalid muid")

// Muid is a 96-bit monotonic, sortable opaque identifier.
type Muid [12]byte

type generator struct {
	mu   sync.Mutex
	last int64
	tail uint32
}

var globalGen generator

// New returns a new strictly monotonic identifier for this process.
func New() Muid {
	return globalGen.next(time.Now().UnixNano())
}

// NewString returns the canonical text form of a new identifier.
func NewString() string {
	return New().String()
}

func (g *generator) next(now int64) Muid {
	g.mu.Lock()
	defer g.mu.Unlock()

	if now > g.last {
		g.last = now
		g.tail = rand.Uint32()
	} else {
		g.tail++
		if g.tail == 0 {
			g.last++
			g.tail = rand.Uint32()
		}
	}

	return muidFromParts(g.last, g.tail)
}

func muidFromParts(ns int64, tail uint32) Muid {
	var m Muid
	binary.BigEndian.PutUint64(m[:8], uint64(ns))
	binary.BigEndian.PutUint32(m[8:], tail)
	return m
}

// Parse parses a 19-character muid text value. It accepts uppercase letters and
// maps i and l to 1 and o to 0; therefore Parse(s).String() may differ from s.
func Parse(s string) (Muid, error) {
	if len(s) != 19 {
		return Muid{}, invalid("invalid muid text length")
	}

	var m Muid
	for i := 0; i < len(s); i++ {
		digit, ok := decodeDigit(s[i])
		if !ok {
			return Muid{}, invalid("invalid muid text character")
		}

		for j := 0; j < 5; j++ {
			if digit&(1<<uint(4-j)) == 0 {
				continue
			}

			bit := 94 - i*5 - j
			m[11-bit/8] |= 1 << uint(bit%8)
		}
	}

	return m, nil
}

// MustParse parses a 19-character muid text value and panics if it is invalid.
func MustParse(s string) Muid {
	m, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return m
}

// String returns the canonical 19-character Crockford Base32 form.
func (m Muid) String() string {
	var text [19]byte
	m.encode(&text)
	return string(text[:])
}

// AppendText appends the canonical text form to dst.
func (m Muid) AppendText(dst []byte) ([]byte, error) {
	var text [19]byte
	m.encode(&text)
	return append(dst, text[:]...), nil
}

func (m Muid) encode(text *[19]byte) {
	for i := range text {
		bit := 94 - i*5
		var digit byte
		for j := 0; j < 5; j++ {
			position := bit - j
			digit = digit<<1 | (m[11-position/8]>>uint(position%8))&1
		}
		text[i] = alphabet[digit]
	}
}

// Time returns the time encoded in m.
func (m Muid) Time() time.Time {
	return time.Unix(0, int64(binary.BigEndian.Uint64(m[:8])))
}

// IsZero reports whether m is the zero identifier.
func (m Muid) IsZero() bool {
	return m == Muid{}
}

// Compare compares m and other in binary sort order.
func (m Muid) Compare(other Muid) int {
	for i := range m {
		if m[i] < other[i] {
			return -1
		}
		if m[i] > other[i] {
			return 1
		}
	}
	return 0
}

// MarshalText returns the canonical text form.
func (m Muid) MarshalText() ([]byte, error) {
	var text [19]byte
	m.encode(&text)
	return append([]byte(nil), text[:]...), nil
}

// UnmarshalText parses text into m.
func (m *Muid) UnmarshalText(text []byte) error {
	if m == nil {
		return invalid("nil muid receiver")
	}

	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// MarshalBinary returns the raw 12-byte representation.
func (m Muid) MarshalBinary() ([]byte, error) {
	return append([]byte(nil), m[:]...), nil
}

// UnmarshalBinary decodes the raw 12-byte representation into m.
func (m *Muid) UnmarshalBinary(data []byte) error {
	if m == nil {
		return invalid("nil muid receiver")
	}
	if len(data) != len(Muid{}) {
		return invalid("invalid muid binary length")
	}
	if data[0]&0x80 != 0 {
		return invalid("invalid muid binary timestamp")
	}

	var parsed Muid
	copy(parsed[:], data)
	*m = parsed
	return nil
}

// Value returns the canonical text form for database storage.
func (m Muid) Value() (driver.Value, error) {
	return m.String(), nil
}

// Scan decodes a text or raw binary database value into m.
func (m *Muid) Scan(src any) error {
	if m == nil {
		return invalid("nil muid receiver")
	}

	switch value := src.(type) {
	case string:
		parsed, err := Parse(value)
		if err != nil {
			return err
		}
		*m = parsed
		return nil
	case []byte:
		switch len(value) {
		case 19:
			parsed, err := Parse(string(value))
			if err != nil {
				return err
			}
			*m = parsed
			return nil
		case 12:
			if value[0]&0x80 != 0 {
				return invalid("invalid muid binary timestamp")
			}
			var parsed Muid
			copy(parsed[:], value)
			*m = parsed
			return nil
		default:
			return invalid("invalid muid scan byte length")
		}
	default:
		return invalid("invalid muid scan type")
	}
}

func decodeDigit(c byte) (byte, bool) {
	digit := decodeTable[c]
	return digit, digit != invalidDigit
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, message)
}
