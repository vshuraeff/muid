# µID: A 96-Bit Monotonic Sortable Identifier Format

```
Document:   µID Format Specification
Category:   Informational (format specification)
Revision:   2 (widened value range, see Section 12.1)
Author:     V. Shuraeff
Date:       August 2026
```

## Abstract

This document specifies µID, a 96-bit identifier with a fixed 12-octet binary form and a
fixed 16-character base62 text form. A µID carries an unsigned 64-bit Unix-nanosecond
timestamp, a 16-bit random field, and a 16-bit CRC-16/CCITT-FALSE checksum over the two
preceding fields. An identifier is valid when its 96-bit value is below 62^16 and its checksum
matches. Binary order, numeric order, and text lexicographic order coincide. This document
defines the binary layout, the checksum, the text encoding and decoding, the validation rules,
the generation algorithm, and the conformance requirements, in sufficient detail to produce
byte-identical and character-identical results in any implementation language.

## Status of This Document

This document is a format specification. It is not a product of any standards body and has no
standards-track status. Section 1.3, Sections 2 through 12, and Appendix A are normative.
Section 1.1, Section 13, and every section or appendix marked "Informative" are not. Section
1.2 defines the requirements language the normative sections use.

The Go package `github.com/vshuraeff/muid` is the reference implementation. It is authoritative
for the wire format — the octets, the canonical text, the checksum, and the accept and reject
outcomes of Section 5 — and for generator behavior below the exhaustion boundary of Section
6.2.1. Where this document and the implementation disagree within that scope, the
implementation defines the format and this document is in error. At and beyond that boundary
this document is authoritative; the implementation is known to deviate there, as Section 6.2.1
records.

## Table of Contents

```
1.  Introduction
    1.1.  Design Goals (Informative)
    1.2.  Requirements Language
    1.3.  Terminology
2.  Binary Format
    2.1.  Field Layout
    2.2.  Bit Diagram
    2.3.  Timestamp Field
    2.4.  Random Field
    2.5.  Checksum Field
    2.6.  Value Bound
3.  Checksum Algorithm
    3.1.  Parameters
    3.2.  Reference Algorithm
4.  Text Format
    4.1.  Alphabet
    4.2.  Encoding
    4.3.  Decoding
    4.4.  Canonical Form
5.  Validation
    5.1.  Text Validation
    5.2.  Binary Validation
    5.3.  Atomicity
6.  Generation
    6.1.  Generator State
    6.2.  Algorithm
          6.2.1.  Value Range Exhaustion
    6.3.  Concurrency
    6.4.  Random Field Source
    6.5.  Resulting Properties
7.  Ordering and Comparison
8.  Range and Lifetime Limits
9.  Conformance
    9.1.  Encoder Conformance
    9.2.  Decoder Conformance
    9.3.  Generator Conformance
10. Security Considerations
11. IANA Considerations
12. Versioning and Stability
    12.1.  Relationship to the Previous Rules (Informative)
13. References
Appendix A.  Test Vectors
Appendix B.  Encoding Walk-Through (Informative)
```

## 1. Introduction

A µID is a 96-bit value used as a database key or an event identifier. Its binary form is 12
octets; its canonical text form is exactly 16 characters drawn from a case-sensitive base62
alphabet. The value is composed of a timestamp, a random field, and a checksum, in that order,
so that ordering by the identifier is ordering by creation time, and so that a corrupted or
mistyped identifier is rejected rather than accepted as a different valid identifier.

### 1.1. Design Goals (Informative)

- A text form short enough to use in URLs and logs: 16 characters, against 36 for a UUID.
- Identical ordering in three domains: octet order, integer order, and text lexicographic
  order under a binary collation.
- Nanosecond time resolution, with strict monotonicity within a generating process even when
  the wall clock moves backwards.
- Self-validation: accidental corruption of a transported identifier is detected at parse time
  rather than silently accepted.
- No configured node identity, and therefore no coordination between generators and no node
  identity to leak.

### 1.2. Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT",
"RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted
as described in BCP 14 [RFC2119] [RFC8174] when, and only when, they appear in all capitals,
as shown here.

### 1.3. Terminology

The format is named µID, where µ is U+00B5 MICRO SIGN, not U+03BC GREEK SMALL LETTER MU. The
ASCII name `muid` designates the code artifacts that implement the format, such as the Go
module, package, and binary, and MAY be used wherever non-ASCII text is impractical. The two
spellings denote the same format; neither is a variant, a version, or a profile of the other.
Machine-consumed literals, including identifiers, module paths, file names, commands, and the
test vectors of Appendix A, use ASCII `muid` only, and MUST NOT contain µ.

- **octet**: an 8-bit byte.
- **identifier value**: the 96-bit unsigned integer formed by the 12 octets of the binary
  form, most significant octet first.
- **canonical text**: the 16-character text form produced by the encoder of Section 4.2.
- **encoder**: a component that converts a binary form to canonical text.
- **decoder**: a component that converts text or binary input to an identifier value, applying
  the validation rules of Section 5.
- **generator**: a component that produces new identifiers per Section 6.
- **Unix time**: elapsed time since 1970-01-01T00:00:00Z, excluding leap seconds.

Bit positions of the identifier value are numbered from 95 (most significant) down to 0 (least
significant). Octet positions are numbered from 0 (first, most significant) to 11.

## 2. Binary Format

The binary form is exactly 12 octets. All multi-octet fields are unsigned and big-endian
(network byte order).

### 2.1. Field Layout

| Octets | Value bits | Width   | Field     | Encoding                                    |
| ------ | ---------- | ------- | --------- | ------------------------------------------- |
| 0..7   | 95..32     | 64 bits | timestamp | unsigned big-endian                         |
| 8..9   | 31..16     | 16 bits | random    | unsigned big-endian; opaque to decoders     |
| 10..11 | 15..0      | 16 bits | checksum  | unsigned big-endian CRC-16 over octets 0..9 |

No bit is reserved or fixed. The fields are not independently bounded; the composed 96-bit
value is bounded by Section 2.6.

### 2.2. Bit Diagram

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                 timestamp: value bits 95..64                  |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                 timestamp: value bits 63..32                  |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|         random field          |            crc-16             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

The ruler numbers bit positions in transmission order within each 32-bit word, position 0
being the most significant bit of that word. Field descriptions use identifier value bit
numbers as defined in Section 1.3.

### 2.3. Timestamp Field

Octets 0..7 hold a count of nanoseconds of Unix time. The field MUST be interpreted as an
unsigned 64-bit big-endian integer. It has no sign bit and no reserved bit; a value with octet
0 above 0x7F is ordinary, and an implementation MUST NOT read the field as a signed integer,
which would misorder every timestamp at or above 2^63 (Section 7). The field's own width
permits 0 to 2^64-1; the reachable range is narrower because Section 2.6 bounds the composed
value.

The nanosecond is the storage unit of the field, not a statement about clock resolution: the
field holds whatever the generator's clock reported, possibly adjusted under Section 6.2, and
on many hosts that clock is coarser than a nanosecond.

Informative: a language whose native instant type is a signed 64-bit nanosecond count, as Go's
`time.Time.UnixNano` is, covers a narrower range than this field, roughly the years 1678
through 2262. Converting a timestamp outside that range through such a type is lossy or
undefined. Implementations should map the field to an instant by splitting it into whole
seconds and a nanosecond remainder, and should expose the raw unsigned 64-bit value for callers
that need it.

### 2.4. Random Field

Octets 8..9 hold a 16-bit value that disambiguates identifiers sharing a timestamp. Decoders
MUST treat this field as opaque: it carries no structure, no ordering meaning beyond its
numeric value, and no identity information. Encoders and decoders MUST preserve it verbatim.
Its production is specified in Section 6.

### 2.5. Checksum Field

Octets 10..11 hold the CRC-16 defined in Section 3, computed over octets 0..9 in order, stored
big-endian. The field is a deterministic function of the preceding 10 octets and therefore
contributes no entropy and no uniqueness.

### 2.6. Value Bound

The 96-bit identifier value MUST be strictly less than 62^16, which is
47672401706823533450263330816 in decimal and `9a09afbae83050a9de010000` as 12 octets. The bound
itself is not a valid identifier.

62^16 is the number of distinct 16-digit base62 strings, so the bound makes the value space and
the text space (Section 4) exactly the same size: every identifier value has a 16-character
text, and every 16-character text over the alphabet denotes an identifier value.

The bound constrains the composed value, not any field in isolation. The largest timestamp is
therefore reachable only with part of the random field's range; Section 8 gives the numbers.

## 3. Checksum Algorithm

### 3.1. Parameters

The checksum is CRC-16/CCITT-FALSE, also catalogued as CRC-16/IBM-3740 and CRC-16/AUTOSAR
[CRCCAT], with the following parameters:

| Parameter | Value  |
| --------- | ------ |
| width     | 16     |
| poly      | 0x1021 |
| init      | 0xFFFF |
| refin     | false  |
| refout    | false  |
| xorout    | 0x0000 |
| check     | 0x29B1 |

`check` is the checksum of the nine ASCII characters `123456789`. A conforming implementation
MUST reproduce it (Appendix A.1).

Note that "CRC-16/CCITT" without further qualification names a different, reflected algorithm
in common use. Only the parameter set above is valid for this format.

### 3.2. Reference Algorithm

All arithmetic is on a 16-bit unsigned register; `<<` discards bits shifted out of the
register.

```
function crc16(octets):
    crc = 0xFFFF
    for each octet b in octets:
        crc = crc XOR (b << 8)
        repeat 8 times:
            if (crc AND 0x8000) != 0:
                crc = (crc << 1) XOR 0x1021
            else:
                crc = crc << 1
    return crc
```

Table-driven or bit-sliced variants MAY be used if they compute the same function.

## 4. Text Format

### 4.1. Alphabet

The alphabet is the 62 ASCII characters [RFC20]:

```
0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz
```

Digit value 0 is `0` and digit value 61 is `z`; that is, `0`..`9` map to 0..9, `A`..`Z` map to
10..35, and `a`..`z` map to 36..61. The mapping is case-sensitive: implementations MUST NOT
case-fold, MUST NOT treat `0`/`O` or `1`/`l`/`I` as aliases, and MUST NOT apply any Unicode
normalization or confusable mapping.

The code points of the alphabet increase strictly with digit value. This property is required
for Section 7 and MUST NOT be altered by reordering the alphabet.

### 4.2. Encoding

The canonical text is the identifier value written in base 62, most significant digit first,
in exactly 16 digit positions, left-padded with the digit `0`.

```
function encode(octets):                       // exactly 12 octets
    value = unsigned integer formed by octets, most significant octet first
    text  = array of 16 characters
    for i from 15 down to 0:
        text[i] = ALPHABET[value mod 62]
        value   = value div 62
    return text
```

For every value permitted by Section 2.6 the loop terminates with `value` equal to 0, since
16 divisions by 62 exhaust any value below 62^16. The encoder MUST NOT trim leading `0`
characters, MUST NOT emit any other length, and MUST NOT add prefixes, separators, or padding
characters.

### 4.3. Decoding

```
function decode_text(text):
    if length(text) != 16:
        reject                                 // Section 5.1, rule 1
    value = 0
    for each character c in text, left to right:
        if c is not in ALPHABET:
            reject                             // Section 5.1, rule 2
        d = digit value of c in ALPHABET
        value = value * 62 + d
    octets = value written as 12 octets, most significant first, zero-padded on the left
    if crc16(octets[0..9]) != integer formed by octets[10..11] big-endian:
        reject                                 // Section 5.1, rule 3
    return octets
```

The accumulator MUST hold at least 96 bits, and 96 bits are sufficient. After k digits the
accumulator holds at most 62^k - 1, so the largest intermediate and the largest result are both
62^16 - 1 = 47672401706823533450263330815, which is below 2^96 = 79228162514264337593543950336
and below the bound of Section 2.6, 62^16. A text decoder therefore needs no range check: no
intermediate can overflow a 96-bit accumulator, and no 16-character input can decode to a value
the bound rejects. Implementations using a wider accumulator MUST NOT read beyond the low 96
bits, which are always the whole value.

`length` counts characters, each of which is a single octet in the alphabet; input containing
multi-octet sequences fails rule 2.

### 4.4. Canonical Form

Every identifier value has exactly one canonical text: the width is fixed, the padding
character is fixed, no character has an alias, and no case variation is accepted. Encoders MUST
produce that text and decoders MUST accept no other text for the value. Consequently, for every
text `s` accepted by a conforming decoder, `encode(decode_text(s))` is `s` itself.

## 5. Validation

### 5.1. Text Validation

A text value is valid if and only if all of the following hold. A decoder MUST enforce all
three and MUST reject any input failing any of them.

1. Its length is exactly 16 characters.
2. Every character is a member of the alphabet of Section 4.1, compared case-sensitively.
3. The CRC-16 of Section 3 over decoded octets 0..9 equals the unsigned big-endian integer in
   decoded octets 10..11.

There is no range rule here. Rules 1 and 2 already confine the decoded value to
0 through 62^16 - 1, so the bound of Section 2.6 cannot be violated by any input that passes
them; Section 4.3 carries the proof. The two rule sets consequently accept exactly the same
identifiers: a text accepted by Section 5.1 always decodes to octets accepted by Section 5.2,
and every value accepted by Section 5.2 has a canonical text accepted by Section 5.1.

### 5.2. Binary Validation

A binary value is valid if and only if all of the following hold. A decoder accepting binary
input MUST enforce all three.

1. Its length is exactly 12 octets.
2. The 12 octets, read as a 96-bit unsigned big-endian integer, are strictly less than 62^16.
   Equivalently, the input compares less than the 12 octets `9a09afbae83050a9de010000` under an
   octet-wise unsigned comparison.
3. The CRC-16 of Section 3 over octets 0..9 equals the unsigned big-endian integer in octets
   10..11.

Rule 2 applies only to binary input, because binary input is the only way to present a value at
or above the bound. Rule 2 MUST be applied independently of rule 3: it rejects out-of-range
input whether or not the checksum matches. The 12 octets `9a09afbae83050a9de01633c` are an
example that satisfies rule 3 and MUST still be rejected under rule 2.

The bound itself, `9a09afbae83050a9de010000`, is rejected under rule 2. It would fail rule 3 as
well, since the checksum of its first ten octets is 0x633C while its last two octets are
0x0000, but that is a property of this particular value and not the reason for its rejection.

Not every 12-octet pattern is a valid µID. In particular, the all-zero pattern fails rule
5.2.3, and its text form (sixteen `0` characters) fails rule 5.1.3, because the checksum of ten
zero octets is 0xE139 and not 0x0000.

### 5.3. Atomicity

Decoding is all-or-nothing. When any rule of Section 5.1 or 5.2 fails, the decoder MUST NOT
deliver a partially decoded value, and any destination the decoder writes into MUST be left
exactly as it was before the call. A conforming decoder MUST distinguish failure from success
to its caller; the form of the failure report is not specified here.

## 6. Generation

This section specifies how a generator produces new identifiers. An implementation that only
encodes, decodes, or validates existing identifiers is not required to implement it, but an
implementation claiming the guarantees of Section 6.5 MUST implement it as specified.

### 6.1. Generator State

A generator holds two variables:

- `last`: an unsigned 64-bit integer, initially 0, holding the timestamp of the most recently
  emitted identifier.
- `rnd`: an unsigned 16-bit integer, initially 0, holding the random field of the most recently
  emitted identifier.

All arithmetic on `last` and `rnd` is unsigned.

### 6.2. Algorithm

`now` is the current Unix time in nanoseconds as an unsigned value, read once per call, before
the state is inspected. A clock reading that precedes the Unix epoch MUST NOT be converted to
an unsigned timestamp by wrap-around, which would place the identifier near the top of the
range; it MUST be replaced by 0. The reference implementation clamps: it reads a signed
nanosecond count and substitutes 0 for any negative reading.

```
function next(now):
    begin exclusive access to (last, rnd)

    if now > last:
        last = now
        rnd  = random16()
    else:
        rnd = (rnd + 1) mod 2^16
        if rnd == 0:                           // the 16-bit field wrapped
            last = last + 1
            rnd  = random16()

    t = last
    r = rnd

    end exclusive access

    octets[0..7]   = t as unsigned 64-bit big-endian
    octets[8..9]   = r as unsigned 16-bit big-endian
    octets[10..11] = crc16(octets[0..9]) as unsigned 16-bit big-endian

    if octets, as a 96-bit unsigned integer, are not below 62^16:
        emit nothing, see Section 6.2.1

    return octets
```

The emitted timestamp is `last` after the update, not `now`. A generator MUST NOT emit `now`
when `now` is less than or equal to `last`.

#### 6.2.1. Value Range Exhaustion

A clock reading, or a sequence of increments from the wrap branch, can carry the composed value
past the bound of Section 2.6. The value never lands on the bound: a generated identifier
always carries the checksum of its own first ten octets, and the bound's first ten octets
checksum to 0x633C while its last two octets are 0x0000. Exhaustion is therefore the first call
whose composed value would exceed the bound, not a call that reaches it. From that call on no
further identifier can be produced, since Section 5.2 rejects every value that is not below the
bound.

Because the bound constrains the composed value, exhaustion is a property of the identifier
about to be emitted, not of the timestamp alone: at the largest timestamp the generator runs out
partway through the random field's range (Section 8).

Generator behavior once the value range is exhausted is outside the scope of this
specification. A conforming generator MUST NOT emit an identifier that is not below the bound;
at that point it MAY fail the call, abort, block, or signal the exhaustion in any way its
environment allows, and MUST NOT silently emit a non-conforming identifier. Implementations
MUST NOT rely on the wrap-around behavior of any particular integer type at this boundary.

Informative: the reference implementation performs no such check, and the Status of This
Document clause is scoped accordingly. From the state `last` = 11099595973925556393,
`rnd` = 0xDE00 its next call returns `9a09afbae83050a9de01633c`, which Section 5.2 rejects and
Appendix A.4 lists as a rejection vector. This is a known and accepted deviation from the rule
above, taken because a generator tracking a real clock cannot reach the boundary before
2321-09-25T13:06:13.925556393Z. It does not make such an identifier valid: a decoder rejects it
whatever produced it.

### 6.3. Concurrency

All reads and writes of `last` and `rnd`, and the derivation of `t` and `r` from them, MUST be
serialized so that concurrent calls observe a strictly increasing sequence of `(last, rnd)`
pairs and no two calls derive the same pair. The reference implementation serializes with a
process-wide mutex; any mechanism providing the same serialization is acceptable.

The serialization scope defines the uniqueness scope. Identifiers from two generator instances,
whether in one process or in different processes, are not coordinated.

### 6.4. Random Field Source

`random16()` MUST return values uniformly distributed over 0 to 65535.

Each process MUST initialize the state of that source at start-up from fresh entropy, obtained
from the operating system where the platform provides it. Implementations MUST NOT use a fixed
seed, a seed derived from a compile-time constant, or a seed shared between processes, and the
seeding of a process MUST NOT be a deterministic function of its executable, its arguments, or
its start-up environment. Two independently seeded processes can still draw equal values by
chance; the requirement is independent seeding, not distinct output. That residual chance is
exactly the cross-generator probability quantified in Section 6.5.

`random16()` is not required to be a cryptographically secure generator, and implementations
MUST NOT rely on it as one (Section 10).

The reference implementation draws a 32-bit value from the package-level generator of Go's
`math/rand/v2` and takes its low 16 bits. That package documents only that its package-level
generator is seeded randomly; the generator's construction and its entropy path are properties
of the Go runtime rather than of the package API. With the toolchain named in `go.mod` (Go
1.26) that runtime uses a ChaCha8 generator seeded from operating system entropy at process
start, with a time-derived fallback where the operating system supplies none. Those are
toolchain details, subject to change, and nothing in this section depends on them.

### 6.5. Resulting Properties

The properties below hold within one generator, per Section 6.3, for every call that emits an
identifier, that is, for every call up to the exhaustion boundary of Section 6.2.1. None of
them is claimed at or beyond that boundary.

- **Strict monotonicity.** Each call returns an identifier that is strictly greater than the
  previous one under every order of Section 7. Each of the three branches of Section 6.2
  increases the pair `(last, rnd)` in lexicographic order: the first raises `last`, the second
  raises `rnd` with `last` unchanged, and the third raises `last`, which dominates the freshly
  drawn `rnd` even when that draw is lower than the previous one.
- **Uniqueness.** Since `(last, rnd)` strictly increases and the checksum is a function of it,
  no value is ever emitted twice.
- **Clock rollback tolerance.** `last` never decreases, so a backward clock jump of any size
  produces the increment branch instead of a lower identifier. The generator resumes tracking
  the clock when the clock passes `last` again. A rollback consumes random-field values and, on
  each wrap, advances `last` by one nanosecond, so a sustained rollback moves `last` ahead of
  the clock rather than behind it.
- **No pre-epoch output.** The timestamp field cannot express an instant before the epoch, and
  the clamp of Section 6.2 keeps a pre-epoch clock reading from wrapping into a far-future
  timestamp: such a reading becomes `now = 0`, which never exceeds `last` and so takes the
  increment branch. A generator whose clock is wrong in that direction keeps emitting valid,
  strictly increasing identifiers, and resumes tracking the clock once the clock passes `last`.

Across generators, uniqueness is probabilistic. Two identifiers carrying the same timestamp
collide if and only if their random fields are equal, which for two independent draws has
probability 2^-16. Because identifiers after the first in a nanosecond are consecutive rather
than independent, each generator covers a run of consecutive random-field values, and two
generators emitting `k` identifiers each within the same nanosecond produce a colliding pair
with probability (2k-1)/2^16 for k up to 2^15, and with certainty for larger k, where the two
runs together cover the whole field. A generator cannot stay in one nanosecond beyond 2^16
identifiers in any case: the wrap branch advances the timestamp.

## 7. Ordering and Comparison

For any two valid identifiers, these three comparisons yield the same result:

1. Octet-wise unsigned comparison of the 12-octet binary forms.
2. Numeric comparison of the 96-bit identifier values.
3. Character-wise comparison of the canonical texts by ASCII code point, as performed by a
   byte-wise string comparison or a binary/code-point collation.

Comparison 1 equals comparison 2 because the binary form is big-endian and fixed-width.
Comparison 3 equals comparison 2 because the text is fixed-width base62 and the alphabet's code
points increase strictly with digit value (Section 4.1); with a variable-width text or a
reordered alphabet the equality would not hold.

Every comparison MUST be unsigned. An implementation that compares the leading octets as signed
bytes, or the timestamp field as a signed 64-bit integer, orders every identifier from
2262-04-11T23:47:16.854775808Z onward before every earlier one. Languages whose default byte
type is signed require explicit care here.

Comparing the first 10 octets is equivalent to comparing all 12: the checksum is a function of
those 10 octets, so two identifiers agreeing on them are identical.

Within one generator the order is exactly the generation order (Section 6.5). Across
generators, the order is timestamp order, and is therefore only as meaningful as the agreement
between the clocks involved; identifiers sharing a timestamp are ordered by their random
fields, which carry no temporal meaning. Implementations MUST NOT derive a happens-before
relation between identifiers from different generators.

Storing the canonical text in a database preserves this order only under a binary or code-point
collation. Under any other collation the index order is whatever that collation defines.

## 8. Range and Lifetime Limits

| Quantity                                  | Value                                     |
| ----------------------------------------- | ----------------------------------------- |
| Value bound, exclusive (62^16)            | 47672401706823533450263330816             |
| Value bound as 12 octets                  | `9a09afbae83050a9de010000`                |
| Text space (16 base62 digits)             | 47672401706823533450263330816             |
| Maximum timestamp                         | 11099595973925556393 ns                   |
| Maximum instant                           | 2321-09-25T13:06:13.925556393Z            |
| Maximum timestamp with random = 0xFFFF    | 11099595973925556392 ns                   |

The value space and the text space have exactly the same size, so no 16-character string over
the alphabet is out of range and no valid identifier lacks a text form. This is why the range
rule appears only in Section 5.2.

The maximum timestamp is only partly usable. The bound's own octets are
`9a09afbae83050a9de010000`: a timestamp of 11099595973925556393 (`9a09afbae83050a9`) leaves the
remaining four octets bounded by `de010000`, so that timestamp is representable only with a
random field of 0xDE00 or below, whatever the checksum turns out to be. With random = 0xFFFF
the largest usable timestamp is one nanosecond lower, 11099595973925556392. The bound
constrains the composed value; no field has a range of its own.

A generator whose next identifier would fall above the bound can produce no further
identifiers, and Section 6.2.1 states what it MUST NOT do there.

## 9. Conformance

An implementation MAY implement any subset of the three conformance classes below and MUST
state which classes it claims.

### 9.1. Encoder Conformance

An encoder MUST, given a 12-octet value satisfying Section 5.2, produce the canonical text
defined in Sections 4.1, 4.2, and 4.4, and MUST NOT produce any other text for that value. It
MUST reproduce Appendix A.2 and A.3.

### 9.2. Decoder Conformance

A decoder MUST apply every rule of Section 5.1 to text input and, if it accepts binary input,
every rule of Section 5.2 to binary input. It MUST accept the canonical text of every valid
identifier value, MUST reject every input failing any rule, MUST be atomic per Section 5.3, and
MUST NOT case-fold or normalize its input. It MUST reproduce the accept and reject outcomes of
Appendix A.

### 9.3. Generator Conformance

A generator MUST implement Section 6.2 with the state of Section 6.1, MUST serialize state
access per Section 6.3, MUST draw the random field per Section 6.4, and MUST clamp a pre-epoch
clock reading per Section 6.2. Every identifier it emits MUST be one a conforming decoder
accepts, and it MUST provide the within-generator
monotonicity and uniqueness properties of Section 6.5 for every call up to the exhaustion
boundary of Section 6.2.1. At that boundary it MUST stop emitting rather than emit an
identifier a conforming decoder would reject; how it reports the exhaustion is unconstrained.

The reference implementation does not implement that stop, as Section 6.2.1 records. An
implementation that copies its behavior at the boundary is conforming everywhere below the
boundary and non-conforming at it; the deviation does not extend the set of valid identifiers,
which Section 5 fixes independently of what produced them.

## 10. Security Considerations

A µID is an identifier, not a secret, and not a capability.

- **Values are predictable.** Within one nanosecond the random field is incremented, so given
  one identifier the identifiers that follow it in that nanosecond are trivially derivable, and
  the timestamp field is largely predictable from context. A µID MUST NOT be used as a session
  token, a password reset token, a capability URL, or any other bearer credential; such values
  are to be drawn from a cryptographically secure random source instead.
- **16 bits of randomness is not a security boundary.** The random field exists to disambiguate
  identifiers within a nanosecond, not to resist guessing or enumeration. Authorization MUST
  NOT rest on an identifier being hard to guess.
- **The checksum is not authentication.** CRC-16 is an error-detection code with a public,
  keyless definition: anyone can compute a valid checksum for chosen contents. It detects
  accidental corruption of a transported identifier with probability 1 - 2^-16 and provides no
  integrity guarantee against a deliberate modification. A random 16-character string over the
  alphabet passes full validation with probability about 2^-16, roughly 1 in 65536, which is a
  measure of typo detection, not of unforgeability.
- **Creation time is disclosed.** Every identifier reveals its creation instant at nanosecond
  resolution to anyone holding it. Where creation times are sensitive, or where nanosecond
  timing enables side-channel inference, a µID MUST NOT be exposed to untrusted parties.
- **No cross-generator ordering guarantee.** Ordering between identifiers from different
  generators depends on the agreement of independent clocks and MUST NOT be treated as a
  causal or audit-grade ordering.
- **No embedded identity.** The format contains no node, host, or process identifier, so it
  leaks none; the same property is why cross-generator uniqueness is probabilistic
  (Section 6.5) rather than guaranteed.

## 11. IANA Considerations

This document has no IANA actions.

## 12. Versioning and Stability

The format carries no version field and no type field: all 96 bits are assigned by Section 2,
and no bit is reserved. This document describes the only wire format defined for the name µID.

The name identity is the format, not the spelling. µID and `muid` are two spellings of one
name, in the sense of Section 1.3, and a document, library, or column labelled with either
refers to this format.

Any change that removes an input a conforming decoder accepts, or alters which text or octets a
conforming encoder produces, is a different format and MUST be given a different name, meaning
a name distinct from both spellings. It MUST NOT be published as a revision of this document,
and implementations MUST NOT attempt to distinguish it from this format at runtime, because no
field is available to signal it.

Revisions of this document MAY correct errors, sharpen wording, and add test vectors, and MUST
leave the encoding unchanged and the set of accepted inputs no smaller.

### 12.1. Relationship to the Previous Rules (Informative)

An earlier revision of this format read octets 0..7 as a 63-bit timestamp with value bit 95
fixed at 0, and bounded identifier values by 2^95. The current rules are a strict widening of
those: 2^95 = 39614081257132168796771975168 is below 62^16 = 47672401706823533450263330816, so
every identifier valid under the old rules is valid under these, and the encoder is unchanged,
so each such identifier still has exactly the text it had before. The vectors in Appendix A.2
include two identifiers minted under the old rules; they are unchanged here.

The widening is one-directional. Identifiers with a timestamp at or above 2^63, such as
`pWE94k9hlxnYxozN` in Appendix A.2, are valid under these rules and are rejected by a decoder
still implementing the old ones. Identifiers from a current generator are therefore unreadable
by an implementation limited to the old bound, which is a deployment constraint rather than a
property of the format.

## 13. References

### 13.1. Normative References

- [RFC2119] Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14,
  RFC 2119, March 1997.
- [RFC8174] Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words", BCP 14,
  RFC 8174, May 2017.
- [RFC20] Cerf, V., "ASCII format for Network Interchange", RFC 20, October 1969.

### 13.2. Informative References

- [CRCCAT] Cook, G., "Catalogue of parametrised CRC algorithms", CRC RevEng,
  <https://reveng.sourceforge.io/crc-catalogue/16.htm>.
- [MUIDGO] Shuraeff, V., "muid", Go package `github.com/vshuraeff/muid` (reference
  implementation; files `muid.go` and `muid_test.go`).

## Appendix A. Test Vectors

The vectors in A.2 and A.3 were produced by, and verified against, the reference
implementation. Each was constructed from the stated timestamp and random field, checksummed,
encoded, and then decoded back to the same 12 octets with the same timestamp.

### A.1. Checksum Check Values

| Input                                | CRC-16 |
| ------------------------------------ | ------ |
| `123456789` (nine ASCII characters)  | 0x29B1 |
| ten zero octets                      | 0xE139 |

### A.2. Boundary Vectors

| Timestamp (ns)       | Random | Binary form (hex)          | Canonical text     |
| -------------------- | ------ | -------------------------- | ------------------ |
| 0                    | 0x0000 | `00000000000000000000e139` | `0000000000000Ezx` |
| 9223372036854775807  | 0xFFFF | `7fffffffffffffffffff42d5` | `pWE94k9hlxnYxc3R` |
| 9223372036854775808  | 0x0000 | `80000000000000000000050d` | `pWE94k9hlxnYxozN` |
| 11099595973925556392 | 0xFFFF | `9a09afbae83050a8ffff6f0a` | `zzzzzzzzzzvvvln8` |
| 11099595973925556393 | 0xDE00 | `9a09afbae83050a9de00731d` | `zzzzzzzzzzzzzqcH` |

The first four are pinned in the reference implementation's test suite. Row by row:

1. The smallest valid identifier: octets 0..9 are all zero and the checksum follows.
2. The largest identifier with the leading bit clear, at 2262-04-11T23:47:16.854775807Z. It was
   also the maximum under the previous rules (Section 12.1) and is unchanged here.
3. The first identifier with the leading bit set, one nanosecond later, at
   2262-04-11T23:47:16.854775808Z. It exercises the unsigned reading required by Sections 2.3
   and 7: an implementation treating octet 0 or the timestamp as signed sorts it before row 1.
4. The largest timestamp usable with random = 0xFFFF, at 2321-09-25T13:06:13.925556392Z.
5. The largest valid identifier: the maximum timestamp 11099595973925556393 at
   2321-09-25T13:06:13.925556393Z with the largest random field the bound leaves for it, 0xDE00
   (Section 8). No valid identifier exceeds it, and its text is the largest canonical text.

### A.3. Intermediate Vectors

| Timestamp (ns)      | Random | Binary form (hex)          | Canonical text     |
| ------------------- | ------ | -------------------------- | ------------------ |
| 1                   | 0x0001 | `00000000000000010001c628` | `00000000004gfjRI` |
| 1000000000000000000 | 0x0000 | `0de0b6b3a764000000008646` | `5aJltd5zBhNs7lSw` |
| 1726000000123456789 | 0x1234 | `17f3fbdaf9aecd151234749f` | `9dkHK11lJCQBz0VL` |
| 1726000000123456789 | 0x1235 | `17f3fbdaf9aecd15123564be` | `9dkHK11lJCQBzGUo` |

The instants are 1970-01-01T00:00:00.000000001Z, 2001-09-09T01:46:40Z, and, for the last two,
2024-09-10T20:26:40.123456789Z.

The last two vectors are consecutive outputs of the increment branch of Section 6.2 and show
the three orders of Section 7 agreeing: `0x1234` precedes `0x1235` numerically, the binary
forms compare in the same direction at octet 9, and `...z0VL` precedes `...zGUo`
lexicographically because `0` (0x30) precedes `G` (0x47). The checksum is not order-preserving
here (0x749F against 0x64BE), which is harmless because two identifiers that differ at all
already differ before octet 10.

### A.4. Rejection Vectors

Every input below MUST be rejected. Text inputs are checked against Section 5.1, binary inputs
against Section 5.2.

| Domain | Input                        | Defect                                | Failing rule |
| ------ | ---------------------------- | ------------------------------------- | ------------ |
| text   | `0000000000000Ez`            | 15 characters                         | 5.1.1        |
| text   | `0000000000000Ezx0`          | 17 characters                         | 5.1.1        |
| text   | `0000000000000E-x`           | `-` is not in the alphabet            | 5.1.2        |
| text   | `zzzzzzzzzzzzzzzz`           | 62^16-1; in range, checksum mismatch  | 5.1.3        |
| text   | `0000000000000000`           | all-zero value; checksum 0xE139       | 5.1.3        |
| text   | `0000000000000ezx`           | case of `E` flipped                   | 5.1.3        |
| text   | `pWE94k9hlxnYxc3r`           | case of trailing `R` flipped          | 5.1.3        |
| text   | `pWE94k9hlxnYxozn`           | case of trailing `N` flipped          | 5.1.3        |
| binary | `00000000000000010001c6`     | 11 octets                             | 5.2.1        |
| binary | `00000000000000010001c62800` | 13 octets                             | 5.2.1        |
| binary | `9a09afbae83050a9de010000`   | the bound itself                      | 5.2.2        |
| binary | `9a09afbae83050a9de01633c`   | above the bound, checksum correct     | 5.2.2        |
| binary | `ffffffffffffffffffffffff`   | far above the bound                   | 5.2.2        |
| binary | `7fffffffffffffffffff42d4`   | last octet of the checksum altered    | 5.2.3        |

The `zzzzzzzzzzzzzzzz` row is the largest 16-character text. Under the previous rules it was
out of range; under these it decodes to 62^16-1, which is below the bound, and is rejected only
because its checksum does not match. No text input can fail on range (Section 5.1).

The `9a09afbae83050a9de01633c` row carries a correct checksum for its first ten octets and is
still rejected: rule 5.2.2 is independent of rule 5.2.3. It is the smallest 12-octet pattern
above the bound that a checksum test alone would accept.

The three case-flip rows illustrate Section 4.1: a case error is not an alias, it decodes to a
different value, and the checksum then rejects it with probability 1 - 2^-16.

## Appendix B. Encoding Walk-Through (Informative)

Encoding the vector with timestamp 1 and random field 0x0001.

The 10 leading octets are `00000000 00000001 0001`. The checksum of those octets is 0xC628, so
the binary form is `00000000000000010001c628`, whose identifier value is 4295083560.

Applying Section 4.2 from position 15 downwards:

| Position | Value before | Value div 62 | Value mod 62 | Character |
| -------- | ------------ | ------------ | ------------ | --------- |
| 15       | 4295083560   | 69275541     | 18           | `I`       |
| 14       | 69275541     | 1117347      | 27           | `R`       |
| 13       | 1117347      | 18021        | 45           | `j`       |
| 12       | 18021        | 290          | 41           | `f`       |
| 11       | 290          | 4            | 42           | `g`       |
| 10       | 4            | 0            | 4            | `4`       |
| 9..0     | 0            | 0            | 0            | `0`       |

The result is `00000000004gfjRI`. Decoding reverses it: starting from 0 and reading left to
right, ten multiplications by 62 leave the accumulator at 0, then the digits 4, 42, 41, 45, 27,
18 build 4, 290, 18021, 1117347, 69275541, 4295083560, whose last two octets match the checksum
of the first ten.
