# muid

Compact, time-ordered unique identifiers for Go. A muid is 12 bytes wide, prints as exactly
19 lowercase Crockford base32 characters, and sorts in generation order in both its binary
and text forms — so you can use it as a primary key and still get chronological ordering
from a plain index scan.

```
66cjq2y61vdb2p0y2kw
66cjq2y62b582d2z08g
66cjq2y62czv02f2844
```

It carries a 63-bit Unix-nanosecond timestamp plus a 32-bit tail. Compared with a UUIDv7 it
is roughly half the string length (19 characters against 36), timestamps to the nanosecond
rather than the millisecond, and is strictly monotonic within the generating process.

## Install

```sh
go get github.com/vshuraeff/muid
```

The module path is a placeholder until the package is published; until then, use it from a
local checkout with a `replace` directive.

## Usage

### Generate

```go
id := muid.New()          // muid.Muid
s := muid.NewString()     // "66cjq2y61vdb2p0y2kw"

fmt.Println(id)           // String() is the canonical 19-character form
fmt.Println(id.Time())    // the encoded timestamp, nanosecond precision
fmt.Println(id.IsZero())  // false
```

### Parse

```go
id, err := muid.Parse("66cjq2y61vdb2p0y2kw")
if errors.Is(err, muid.ErrInvalid) {
    // bad length or bad character
}

id = muid.MustParse("66cjq2y61vdb2p0y2kw") // panics instead of returning an error
```

`Parse` is lenient in the Crockford spirit: it accepts uppercase letters, maps `i` and `l`
to `1`, and maps `o` to `0`. Round-tripping a non-canonical input therefore normalizes it,
and `Parse(s).String()` may differ from `s`.

### Sort and compare

```go
slices.SortFunc(ids, muid.Muid.Compare)

if a.Compare(b) < 0 {
    // a was generated before b, or in the same nanosecond with a lower tail
}
```

Since the text form is fixed-width base32 over an ordered alphabet, byte-wise string
comparison gives the same order as `Compare`. In a database, a text index reproduces that
order under a binary or code-point collation — PostgreSQL `C`, or `COLLATE "C"` on the
column, and a MySQL `*_bin` collation. Under any other collation the index order is whatever
that collation defines, so either pin the collation or store the raw 12 bytes.

### Appending without allocating

```go
buf = append(buf, "id="...)
buf, _ = id.AppendText(buf) // appends the 19 characters, error is always nil
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
// {"id":"66cjq2y61vdb2p0y2kw","body":"hello"}

var e Event
_ = json.Unmarshal(b, &e)
```

`MarshalBinary` and `UnmarshalBinary` are also available for the raw 12-byte form.

### database/sql

`Value` stores the canonical text; `Scan` accepts either the 19-character text (as `string`
or `[]byte`) or the raw 12 bytes, so the same type works against a `char(19)`, `text`, or
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

## Format

| Property         | Value                                                            |
| ---------------- | ---------------------------------------------------------------- |
| Binary size      | 12 bytes, big-endian                                             |
| Significant bits | 95 (the top bit is always zero)                                  |
| Text length      | exactly 19 characters                                            |
| Alphabet         | `0123456789abcdefghjkmnpqrstvwxyz` (Crockford base32, lowercase) |
| Time resolution  | nanoseconds, valid through roughly the year 2262                 |
| Ordering         | binary and text both sort in generation order                    |

Bit layout, most significant bit first:

| Bits    | Width | Content                                                             |
| ------- | ----- | ------------------------------------------------------------------- |
| 95      | 1     | always zero (the sign bit of the `int64` timestamp)                  |
| 94 – 32 | 63    | Unix nanoseconds, matching `time.Time.UnixNano`                      |
| 31 – 0  | 32    | tail: random at the start of each nanosecond, then incremented       |

19 base32 characters encode 95 bits, which is exactly the significant range, so text and
binary carry the same information with no padding. The 2262 limit is the `UnixNano` range;
past it, both the top-bit-zero invariant and the ordering assumptions stop holding.

## Guarantees

- **Strictly monotonic and unique within a process**, including across a clock rollback.
  Generation is serialized: when the clock does not advance past the last value used, the
  tail is incremented instead, and on tail overflow the timestamp is nudged forward by one
  nanosecond. Two calls in the same process never return the same value and never return a
  value that sorts before an earlier one, whatever the wall clock does.
- **Probabilistic across processes**: roughly a 2^-32 chance of collision per pair of IDs
  generated in the same nanosecond. Different processes do not coordinate, so their relative
  ordering also depends on their clocks being in agreement.

## Non-goals

- **Not a security token.** Within a single nanosecond, tails are sequential rather than
  uniformly random, so given one ID you can guess its neighbours. Never use a muid as a
  session token, password reset key, or capability URL — use `crypto/rand` for those.
- **Nanoseconds are the storage unit, not a guaranteed clock resolution.** The value is as
  precise as `time.Now` on the host, which on many platforms is coarser than a nanosecond;
  treat `Time()` as an ordering key with a timestamp attached, not as a measurement.
- **No embedded machine or process identity.** There is no node ID to configure and none to
  leak, which is also why cross-process uniqueness is probabilistic rather than guaranteed.

## Comparison

| Scheme  | Bits | String length | Time-sortable        |
| ------- | ---- | ------------- | -------------------- |
| UUIDv4  | 128  | 36            | no                   |
| UUIDv7  | 128  | 36            | yes, milliseconds    |
| ULID    | 128  | 26            | yes, milliseconds    |
| xid     | 96   | 20            | yes, seconds         |
| ksuid   | 160  | 27            | yes, seconds         |
| muid    | 96   | 19            | yes, nanoseconds     |

## Command line

```sh
go build ./cmd/muid
```

```sh
muid                        # print one new id
muid -n 5                   # print five new ids, one per line
muid -d 66cjq2y61vdb2p0y2kw # decode an id and print the time it encodes
```
