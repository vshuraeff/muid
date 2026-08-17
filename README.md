# muid

Compact, time-ordered, self-validating unique identifiers for Go. A muid is 12 bytes wide,
prints as exactly 16 case-sensitive base62 characters, and sorts in creation order in both
its binary and its text form — so you can use it as a primary key and still get
chronological ordering from a plain index scan.

Throughout, "creation order" means creation order within one generating process, where it is
exact. Ids from different processes sort by their embedded timestamps, so their relative
order is only as good as the agreement between those clocks, and ids created in the same
nanosecond sort by a random field that carries no temporal meaning.

```
9yrSO26OoIfbvR9t
9yrSO26Qa43u4ogZ
9yrSO26QnToYjAL8
```

Those 12 bytes are a 63-bit Unix-nanosecond timestamp, a 16-bit random field, and a 16-bit
CRC-16/CCITT-FALSE checksum over the other ten bytes. The checksum is what makes a muid
self-validating: `Parse` rejects a corrupted or mistyped id instead of handing back a
plausible-looking wrong one.

Compared with a UUIDv7 it is under half the string length (16 characters against 36),
timestamps to the nanosecond rather than the millisecond, is strictly monotonic within the
generating process, and carries an integrity check.

## Install

```sh
go get github.com/vshuraeff/muid
```

The module path is a placeholder until the package is published; until then, use it from a
local checkout with a `replace` directive.

### Development

The `Makefile` drives the standard Go toolchain and needs no external linters. `make all`
runs lint, tests, and build; `make test` runs the race-enabled suite; `make fuzz` runs the
parse fuzz target; `make bench` runs the benchmarks; `make lint` checks formatting and runs
`go vet`; `make install` puts the `muid` binary into `~/.local/bin`. `make help` lists every
target.

## Usage

### Generate

```go
id := muid.New()          // muid.Muid
s := muid.NewString()     // "9yrSO26OoIfbvR9t"

fmt.Println(id)           // String() is the canonical 16-character form
fmt.Println(id.Time())    // the encoded timestamp, nanosecond precision
fmt.Println(id.IsZero())  // false
```

### Parse

```go
id, err := muid.Parse("9yrSO26OoIfbvR9t")
if errors.Is(err, muid.ErrInvalid) {
    // wrong length, character outside the alphabet, out of range, or bad checksum
}

id = muid.MustParse("9yrSO26OoIfbvR9t") // panics instead of returning an error
```

`Parse` is strict. The input must be exactly 16 characters from the base62 alphabet, and it
is case-sensitive: there are no aliases, no case folding, and no substitutions, so
`Parse(s).String() == s` for every accepted `s`.

### Sort and compare

```go
slices.SortFunc(ids, muid.Muid.Compare)

if a.Compare(b) < 0 {
    // a has the lower id: an earlier timestamp, or the same nanosecond and a lower random field
}
```

Since the text form is fixed-width base62 over an alphabet that is already in ASCII order,
byte-wise string comparison gives the same order as `Compare`. In a database, a text index
reproduces that same order under a binary or code-point collation — PostgreSQL `C`, or
`COLLATE "C"` on the column, and a MySQL `*_bin` collation. Under any other collation the
index order is whatever that collation defines, so either pin the collation or store the raw
12 bytes.

### Appending without allocating

```go
buf = append(buf, "id="...)
buf, _ = id.AppendText(buf) // appends the 16 characters, error is always nil
```

### JSON and other text encodings

`Muid` implements `encoding.TextMarshaler` and `*Muid` implements
`encoding.TextUnmarshaler`, so `encoding/json` encodes a muid as a JSON string and accepts
it as a JSON map key. Other text formats — YAML, URL query binding, config decoders — work
the same way in libraries that honor those two interfaces, which most but not all do:

```go
type Event struct {
    ID   muid.Muid `json:"id"`
    Body string    `json:"body"`
}

b, _ := json.Marshal(Event{ID: muid.New(), Body: "hello"})
// {"id":"9yrSO26R5m4kRHGg","body":"hello"}

var e Event
_ = json.Unmarshal(b, &e)
```

Unmarshalling runs the same checksum verification as `Parse`, so a mangled id in a payload
fails decoding rather than becoming a valid-looking key.

`MarshalBinary` and `UnmarshalBinary` are also available for the raw 12-byte form.

### database/sql

`Value` stores the canonical text; `Scan` accepts either the 16-character text (as `string`
or `[]byte`) or the raw 12 bytes, so the same type works against a `char(16)`, `text`, or
`bytea`/`blob` column:

```go
_, err := db.Exec("insert into events (id, body) values (?, ?)", muid.New(), "hello")

var id muid.Muid
err = db.QueryRow("select id from events where body = ?", "hello").Scan(&id)
```

For nullable columns, wrap it in the generic null type from `database/sql`:

```go
var id sql.Null[muid.Muid]
err = db.QueryRow("select parent_id from events where id = ?", child).Scan(&id)
if id.Valid {
    use(id.V)
}
```

## Self-validation

The last two bytes are a CRC-16/CCITT-FALSE checksum over the first ten — polynomial
`0x1021`, initial value `0xffff`, no reflection and no final xor, the variant whose check
value over `"123456789"` is `0x29b1`.

Every entry point that turns outside data into a `Muid` verifies it: `Parse`, `MustParse`,
`UnmarshalText`, `UnmarshalBinary`, and `Scan`. A truncated copy-paste, a flipped character,
a wrong case, or a corrupted blob is rejected with an error wrapping `ErrInvalid` and is
caught with probability 1 - 2^-16. `UnmarshalBinary` and `Scan` additionally reject binary
input whose top bit is set, since that cannot be a valid timestamp.

Two consequences are worth stating plainly:

- The checksum is deterministic — it is a function of the ten preceding bytes, so it adds
  nothing to uniqueness. Only the timestamp and the random field carry entropy.
- Not every 12-byte pattern is a muid. The zero value encodes as sixteen `0` characters, but
  it is not checksum-valid, so parsing that text fails. Round-tripping is guaranteed for ids
  produced by `New` or accepted by `Parse`, `UnmarshalBinary`, or `Scan`, not for arbitrary
  bytes you construct yourself.

## Format

| Property         | Value                                                                  |
| ---------------- | ---------------------------------------------------------------------- |
| Binary size      | 12 bytes, big-endian                                                   |
| Significant bits | 95 (the top bit is always zero)                                        |
| Text length      | exactly 16 characters, left-padded with `0`                            |
| Alphabet         | `0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz`       |
| Case             | significant; no aliases and no normalization                           |
| Time resolution  | nanoseconds, valid through roughly the year 2262                       |
| Ordering         | binary, text, and numeric order agree; process-local creation order    |

Bit layout, most significant bit first:

| Bits    | Width | Content                                                        |
| ------- | ----- | -------------------------------------------------------------- |
| 95      | 1     | always zero (the sign bit of the `int64` timestamp)             |
| 94 – 32 | 63    | Unix nanoseconds, matching `time.Time.UnixNano`                 |
| 31 – 16 | 16    | random field: random at the start of each nanosecond, then incremented |
| 15 – 0  | 16    | CRC-16/CCITT-FALSE over the first 10 bytes                      |

The text form is the 96-bit value written as a base62 integer, padded to a fixed 16
characters. Sixteen base62 digits span slightly more than 95 bits, so `Parse` rejects text
that decodes to 2^95 or above as out of range. Because the alphabet is listed in ASCII order
and the width is fixed, lexicographic order over the text equals numeric order over the
value. The 2262 limit is the `UnixNano` range; past it, both the top-bit-zero invariant and
the ordering assumptions stop holding.

## Guarantees

- **Strictly monotonic and unique within a process**, including across a clock rollback.
  Generation is serialized: when the clock does not advance past the last value used, the
  random field is incremented instead, and if that field wraps, the timestamp is nudged
  forward by one nanosecond. Two calls in the same process never return the same value and
  never return a value that sorts before an earlier one, whatever the wall clock does.
- **Probabilistic across processes**: roughly a 2^-16 chance of collision per pair of ids
  generated in the same nanosecond by different processes, which do not coordinate. Their
  relative ordering also depends on their clocks being in agreement.

That 2^-16 is a deliberate trade. Schemes with a large random component buy longer odds by
spending bits; muid spends its bits on a nanosecond timestamp, a 16-character text form, and
a checksum instead. Nanosecond timestamps already make same-nanosecond overlap rare between
processes, and the checksum turns silent corruption into a loud error — which, in practice,
is the failure that actually happens. If your threat model is many uncoordinated writers
generating ids at nanosecond alignment, prefer a scheme with a bigger random field.

## Non-goals

- **Not a security token.** Within a single nanosecond, the random field is sequential after
  the first value, so given one id the next ones are guessable, and the checksum is
  computable by anyone. Never use a muid as a session token, password reset key, or
  capability URL — use `crypto/rand` for those.
- **Nanoseconds are the storage unit, not a guaranteed clock resolution.** The value is as
  precise as `time.Now` on the host, which on many platforms is coarser than a nanosecond;
  treat `Time()` as an ordering key with a timestamp attached, not as a measurement.
- **No embedded machine or process identity.** There is no node id to configure and none to
  leak, which is also why cross-process uniqueness is probabilistic rather than guaranteed.

## Comparison

| Scheme  | Bits | String length | Time-sortable      | Self-validating |
| ------- | ---- | ------------- | ------------------ | --------------- |
| UUIDv4  | 128  | 36            | no                 | no              |
| UUIDv7  | 128  | 36            | yes, milliseconds  | no              |
| ULID    | 128  | 26            | yes, milliseconds  | no              |
| xid     | 96   | 20            | yes, seconds       | no              |
| ksuid   | 160  | 27            | yes, seconds       | no              |
| muid    | 96   | 16            | yes, nanoseconds   | yes, CRC-16     |

muid and xid carry the same 96 bits; muid prints them in four fewer characters because
base62 packs more per character than base32, and it spends 16 of those bits on the checksum
that the others do not have.

## Command line

```sh
go build ./cmd/muid
```

```sh
muid                     # print one new id
muid -n 5                # print five new ids, one per line
muid -d 9yrSO26OoIfbvR9t # decode an id
```

`-d` verifies the checksum and then prints the id, the time it encodes (RFC 3339 with
nanoseconds, plus the raw Unix-nanosecond value), its random field, and its checksum. An id
that fails verification is reported as an error instead.

## License

MIT — see `LICENSE`.
