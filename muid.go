// Package muid implements a 96-bit monotonic, sortable opaque identifier.
// Its 12-byte binary form is a 63-bit-effective, big-endian int64 Unix-nanoseconds
// timestamp, followed by a 16-bit random field and a 16-bit CRC-16/CCITT-FALSE
// checksum over the first 10 bytes. Its text form is exactly 16 case-sensitive
// base62 characters, fixed-width and left-padded with '0'. Text lexicographic
// order, numeric order, and binary byte order all equal creation order.
//
// Muid is strictly monotonic within one process, including across backward clock
// jumps. Two independently generated ids in the same nanosecond have roughly a
// 2^-16 collision probability. The checksum detects accidental corruption of
// parsed text with probability 1 - 2^-16, but provides no uniqueness benefit.
// Muid is not a security token: values within one nanosecond are sequential after
// the first random field rather than independently random. Nanoseconds are the
// storage unit, not a guaranteed clock resolution. The top-bit-zero timestamp
// invariant is valid through roughly the year 2262, matching time.Time.UnixNano.
//
// The zero Muid encodes as sixteen '0' characters, but it is not CRC-valid, so
// Parse of that text returns a checksum error. String round trips are guaranteed
// only for ids produced by New or accepted by Parse, UnmarshalBinary, or Scan;
// arbitrary byte patterns, including the zero value, are not necessarily valid.
package muid

import (
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	alphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	invalidDigit = 0xff
	textLength   = 16
)

var decodeTable = func() [256]byte {
	var table [256]byte
	for i := range table {
		table[i] = invalidDigit
	}
	for i := range alphabet {
		table[alphabet[i]] = byte(i)
	}
	return table
}()

var crcTable = func() [256]uint16 {
	var table [256]uint16
	for i := range table {
		crc := uint16(i) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}()

// ErrInvalid reports invalid muid text, binary data, or database input.
var ErrInvalid = errors.New("invalid muid")

// Muid is a 96-bit monotonic, sortable opaque identifier.
type Muid [12]byte

type generator struct {
	mu   sync.Mutex
	last int64
	rnd  uint16
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
		g.rnd = uint16(rand.Uint32())
	} else {
		g.rnd++
		if g.rnd == 0 {
			g.last++
			g.rnd = uint16(rand.Uint32())
		}
	}

	return newMuid(g.last, g.rnd)
}

func newMuid(ns int64, rnd uint16) Muid {
	var m Muid
	binary.BigEndian.PutUint64(m[:8], uint64(ns))
	binary.BigEndian.PutUint16(m[8:10], rnd)
	binary.BigEndian.PutUint16(m[10:12], crc16(m[:10]))
	return m
}

func crc16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc = crc<<8 ^ crcTable[byte(crc>>8)^b]
	}
	return crc
}

// Parse parses a 16-character, case-sensitive base62 muid text value.
func Parse(s string) (Muid, error) {
	if len(s) != textLength {
		return Muid{}, invalid("invalid muid text length")
	}

	var hi, lo uint64
	for i := range s {
		digit := decodeTable[s[i]]
		if digit == invalidDigit {
			return Muid{}, invalid("invalid muid text character")
		}

		loHi, newLo := bits.Mul64(lo, 62)
		hiHi, hiLo := bits.Mul64(hi, 62)
		newHi, carry := bits.Add64(loHi, hiLo, 0)
		if hiHi != 0 || carry != 0 {
			return Muid{}, invalid("value out of range")
		}
		newLo, carry = bits.Add64(newLo, uint64(digit), 0)
		newHi, carry = bits.Add64(newHi, 0, carry)
		if carry != 0 {
			return Muid{}, invalid("value out of range")
		}
		hi, lo = newHi, newLo
	}

	if hi >= 1<<31 {
		return Muid{}, invalid("value out of range")
	}

	var parsed Muid
	binary.BigEndian.PutUint32(parsed[:4], uint32(hi))
	binary.BigEndian.PutUint64(parsed[4:], lo)
	if crc16(parsed[:10]) != binary.BigEndian.Uint16(parsed[10:12]) {
		return Muid{}, invalid("checksum mismatch")
	}
	return parsed, nil
}

// MustParse parses a 16-character muid text value and panics if it is invalid.
func MustParse(s string) Muid {
	m, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return m
}

// String returns the canonical 16-character base62 form.
func (m Muid) String() string {
	var text [textLength]byte
	m.encode(&text)
	return string(text[:])
}

// AppendText appends the canonical text form to dst.
func (m Muid) AppendText(dst []byte) ([]byte, error) {
	var text [textLength]byte
	m.encode(&text)
	return append(dst, text[:]...), nil
}

func (m Muid) encode(text *[textLength]byte) {
	hi := binary.BigEndian.Uint32(m[:4])
	lo := binary.BigEndian.Uint64(m[4:])
	for i := len(text) - 1; i >= 0; i-- {
		qHi := hi / 62
		remainder := hi % 62
		qLo, digit := bits.Div64(uint64(remainder), lo, 62)
		text[i] = alphabet[digit]
		hi, lo = qHi, qLo
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
	var text [textLength]byte
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

	parsed, err := parseBinary(data)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func parseBinary(data []byte) (Muid, error) {
	if len(data) != len(Muid{}) {
		return Muid{}, invalid("invalid muid binary length")
	}
	if data[0]&0x80 != 0 {
		return Muid{}, invalid("invalid muid binary timestamp")
	}
	if crc16(data[:10]) != binary.BigEndian.Uint16(data[10:12]) {
		return Muid{}, invalid("checksum mismatch")
	}

	var parsed Muid
	copy(parsed[:], data)
	return parsed, nil
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
		if len(value) == textLength {
			parsed, err := Parse(string(value))
			if err != nil {
				return err
			}
			*m = parsed
			return nil
		}
		parsed, err := parseBinary(value)
		if err != nil {
			return err
		}
		*m = parsed
		return nil
	default:
		return invalid("invalid muid scan type")
	}
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, message)
}
