// Package muid implements µID, a 96-bit monotonic, sortable opaque identifier.
// Its 12-byte binary form is an unsigned 64-bit, big-endian Unix-nanoseconds
// timestamp, followed by a 16-bit random field and a 16-bit CRC-16/CCITT-FALSE
// checksum over the first 10 bytes. A µID is valid when its 96-bit value is
// less than 62^16 and its checksum matches. Its text form is exactly 16
// case-sensitive base62 characters, fixed-width and left-padded with '0'. Text
// lexicographic order, numeric order, and binary byte order all equal creation
// order.
//
// µID is strictly monotonic within one process, including across backward clock
// jumps. Two independently generated ids in the same nanosecond have roughly a
// 2^-16 collision probability. The checksum detects accidental corruption of
// parsed text with probability 1 - 2^-16, but provides no uniqueness benefit.
// µID is not a security token: values within one nanosecond are sequential after
// the first random field rather than independently random. Nanoseconds are the
// storage unit, not a guaranteed clock resolution. Timestamps through roughly
// the year 2321 are representable. This is a strict widening of the previous
// 2^95-bounded, top-bit-zero rules: every identifier valid under those rules
// remains valid and encodes identically.
//
// The zero µID encodes as sixteen '0' characters, but it is not CRC-valid, so
// Parse of that text returns a checksum error. String round trips are guaranteed
// only for ids produced by New or accepted by Parse, UnmarshalBinary, or Scan;
// arbitrary byte patterns, including the zero value, are not necessarily valid.
package muid

import (
	"bytes"
	"database/sql/driver"
	"encoding/binary"
	"errors"
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

var maxValidPlusOne = [12]byte{0x9a, 0x09, 0xaf, 0xba, 0xe8, 0x30, 0x50, 0xa9, 0xde, 0x01, 0x00, 0x00}

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

// ErrInvalid reports invalid µID text, binary data, or database input.
var ErrInvalid = errors.New("invalid muid")

type invalidError struct{ message string }

func (e *invalidError) Error() string {
	return e.message
}

func (e *invalidError) Unwrap() error {
	return ErrInvalid
}

var (
	errInvalidTextLength  = &invalidError{"invalid muid: invalid muid text length"}
	errInvalidTextChar    = &invalidError{"invalid muid: invalid muid text character"}
	errChecksumMismatch   = &invalidError{"invalid muid: checksum mismatch"}
	errNilReceiver        = &invalidError{"invalid muid: nil muid receiver"}
	errInvalidBinaryLen   = &invalidError{"invalid muid: invalid muid binary length"}
	errInvalidBinaryValue = &invalidError{"invalid muid: invalid muid binary value"}
	errInvalidScanType    = &invalidError{"invalid muid: invalid muid scan type"}
)

// Muid is the 12-byte binary form of a µID, a 96-bit monotonic, sortable
// opaque identifier.
type Muid [12]byte

type generator struct {
	mu   sync.Mutex
	last uint64
	rnd  uint16
}

var globalGen generator

func clampNanos(ns int64) uint64 {
	if ns < 0 {
		return 0
	}
	return uint64(ns)
}

// New returns a new strictly monotonic identifier for this process.
func New() Muid {
	return globalGen.next(clampNanos(time.Now().UnixNano()))
}

// NewString returns the canonical text form of a new identifier.
func NewString() string {
	return New().String()
}

func (g *generator) next(now uint64) Muid {
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

func newMuid(ns uint64, rnd uint16) Muid {
	var m Muid
	binary.BigEndian.PutUint64(m[:8], ns)
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

// Parse parses a 16-character, case-sensitive base62 µID text value.
func Parse(s string) (Muid, error) {
	if len(s) != textLength {
		return Muid{}, errInvalidTextLength
	}

	var hi, lo uint64
	for i := range s {
		digit := decodeTable[s[i]]
		if digit == invalidDigit {
			return Muid{}, errInvalidTextChar
		}

		loHi, newLo := bits.Mul64(lo, 62)
		_, hiLo := bits.Mul64(hi, 62)
		newHi, _ := bits.Add64(loHi, hiLo, 0)
		newLo, carry := bits.Add64(newLo, uint64(digit), 0)
		newHi, _ = bits.Add64(newHi, 0, carry)
		hi, lo = newHi, newLo
	}

	var parsed Muid
	binary.BigEndian.PutUint32(parsed[:4], uint32(hi))
	binary.BigEndian.PutUint64(parsed[4:], lo)
	if crc16(parsed[:10]) != binary.BigEndian.Uint16(parsed[10:12]) {
		return Muid{}, errChecksumMismatch
	}
	return parsed, nil
}

// MustParse parses a 16-character µID text value and panics if it is invalid.
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
// Calling UnixNano on the result is undefined for timestamps outside its
// representable range, roughly the years 1678 through 2262. Callers that need
// the raw 64-bit nanosecond value outside that range should decode the first
// eight bytes of MarshalBinary's result, or m[:8], as a big-endian uint64.
func (m Muid) Time() time.Time {
	ns := binary.BigEndian.Uint64(m[:8])
	return time.Unix(int64(ns/1_000_000_000), int64(ns%1_000_000_000))
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
		return errNilReceiver
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
		return errNilReceiver
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
		return Muid{}, errInvalidBinaryLen
	}
	if bytes.Compare(data, maxValidPlusOne[:]) >= 0 {
		return Muid{}, errInvalidBinaryValue
	}
	if crc16(data[:10]) != binary.BigEndian.Uint16(data[10:12]) {
		return Muid{}, errChecksumMismatch
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
		return errNilReceiver
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
		return errInvalidScanType
	}
}
