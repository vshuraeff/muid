# µID in PostgreSQL

`postgres/muid.sql` implements µID inside PostgreSQL in plain SQL and PL/pgSQL: encode,
decode, validate, generate, and two domains to store ids in. There is no extension to
install and no C to compile — loading the file into a database is the whole installation.
[SPEC.md](SPEC.md) defines the format normatively; this page covers the implementation, what
it costs, and where it departs from the specification.

It claims encoder and decoder conformance (SPEC.md sections 9.1 and 9.2), checked against
every vector in Appendix A, against 1000 ids produced by the Go implementation, and with a
10 000-id ordering test in which `ORDER BY` on both domains reproduced the Go generation
order. It does not claim generator conformance (9.3): `muid_new()` keeps none of the generator
state of section 6.1 — see [Semantics and caveats](#semantics-and-caveats).

## Quick start

```sh
psql -d yourdb -f postgres/muid.sql
```

```sql
CREATE TABLE events (
  id   muid PRIMARY KEY DEFAULT muid_new(),
  body text NOT NULL
);

INSERT INTO events (body) VALUES ('signup'), ('login');

SELECT muid_encode(id) AS id, muid_time(id) AS created, body
FROM events
ORDER BY id;
```

```
        id        |            created            |  body
------------------+-------------------------------+--------
 9yrvs5CbHGrkVZEV | 2026-08-17 19:17:00.397064+00 | signup
 9yrvs5Cxk3XkomJH | 2026-08-17 19:17:00.398206+00 | login
```

An id that arrived as text — from an application, a log line, an import file — is converted
with `muid_decode`, and back with `muid_encode`:

```sql
INSERT INTO events (id, body) VALUES (muid_decode('9yrvdD9PezhFdxpz'), 'imported');

SELECT body FROM events WHERE id = muid_decode('9yrvdD9PezhFdxpz');
```

Outside data is checked with `muid_is_valid`, which returns false for any invalid input
instead of raising:

```sql
SELECT muid_is_valid('9yrvdD9PezhFdxpz') AS ok,
       muid_is_valid('9yrvdD9PezhFdxpZ') AS one_character_changed;
```

```
 ok | one_character_changed
----+-----------------------
 t  | f
```

`muid_decode` verifies the same rules and raises on a failure, so a mistyped id cannot become
plausible-looking octets:

```sql
SELECT muid_decode('9yrvdD9PezhFdxpZ');
-- ERROR:  muid: checksum mismatch in 9yrvdD9PezhFdxpZ
```

The domains run `muid_is_valid` as their `CHECK`, so a bad id cannot enter a column either:

```sql
INSERT INTO events (id, body) VALUES ('\x000000000000000000000000', 'zero');
-- ERROR:  value for domain muid violates check constraint "muid_check"
```

For a column that is meant to be read by people or pasted into a URL, use the text domain:

```sql
CREATE TABLE tokens (
  id   muid_text PRIMARY KEY DEFAULT muid_new_text(),
  body text NOT NULL
);
```

Loading `postgres/muid.sql` a second time into a database whose tables already use the
domains stops at `DROP DOMAIN` instead of dropping those columns; drop or convert the
dependent columns first, or load only the function definitions.

## Storage

`muid` over `bytea` is the form to reach for. It is smaller, its `CHECK` is about eight times
cheaper, and its ordering does not depend on a collation. `muid_text` over
`text COLLATE "C"` exists for columns whose readability matters more than either.

|                              | `muid` (bytea)               | `muid_text` (text COLLATE "C")     |
| ---------------------------- | ---------------------------- | ---------------------------------- |
| Stored width per value       | 13 bytes                     | 17 bytes                           |
| 100k rows, table + index     | 7472 kB                      | 9120 kB                            |
| `INSERT`, generated ids      | 7.6 µs/row                   | 41.0 µs/row                        |
| Domain `CHECK`               | 2.1 µs/row                   | 16.7 µs/row                        |
| Ordering                     | octet-wise, collation-free   | needs `COLLATE "C"`                |

The `CHECK` gap is inherent, not an implementation detail: the checksum is defined over the
binary form, so validating text has to base62-decode all 16 characters before it can compute
anything. Most of the rest of the `INSERT` gap is `muid_new_text()` paying for that same
encode. Building the index took 76 ms against 81 ms for 100 000 rows, and `ORDER BY` 6 ms
against 12 ms — the same order, with the text form slightly behind.

The collation matters because SPEC.md section 7 equates text order with numeric order for
*code-point* comparison. A libc or ICU collation may order the 16 characters differently, and
then an index scan no longer returns creation order. The `muid_text` domain pins `COLLATE "C"`
so a column declared with it is safe whatever the database default is; a plain `text` column
holding µIDs is not.

No custom operator class is needed for either domain. `bytea` compares octet by octet and
`text COLLATE "C"` compares by code point, and SPEC.md section 7 states that both orders equal
numeric order over the identifier value, so a plain B-tree index already returns creation
order.

Both domains accept `NULL`, as domains do; add `NOT NULL` where a column must carry an id.

## Using the Go library

Reads need nothing special from either domain: `Muid.Scan` takes the raw 12 octets and the
16-character text alike, and the drivers hand a `muid` column back as 12 octets and a `muid_text`
column — or `muid_encode(id)` — back as text. Writes are where the domains differ, because
`Muid.Value` returns the text form, which is what `muid_text` stores and not what `muid` stores.
So `muid` keeps the storage advantages above at the price of one conversion on every parameter
passed as a `Muid`, and `muid_text` trades those advantages for Go code that binds ids as they are.

The behavior below was checked on PostgreSQL 18.4 against `jackc/pgx/v5` v5.10.0 and `lib/pq`
v1.11.1.

A `muid_text` column takes the Go value directly and gives it back:

```go
id := muid.New()

if _, err := db.ExecContext(ctx,
    `INSERT INTO tokens (id, body) VALUES ($1, $2)`, id, "signup"); err != nil {
    return err
}

var got muid.Muid
if err := db.QueryRowContext(ctx,
    `SELECT id FROM tokens WHERE id = $1`, id).Scan(&got); err != nil {
    return err
}
```

The same `INSERT` against the `muid` column fails, and the error says why:

```
ERROR:  value for domain muid violates check constraint "muid_check"
```

PostgreSQL read the 16 characters `Value` returned as `bytea` input, which makes 16 octets rather
than the 12 the domain requires, and the `CHECK` rejected the row. Decode at the SQL boundary
instead — this form works on every driver tested:

```go
id := muid.New()

if _, err := db.ExecContext(ctx,
    `INSERT INTO events (id, body) VALUES (muid_decode($1), $2)`, id, "signup"); err != nil {
    return err
}

var got muid.Muid
if err := db.QueryRowContext(ctx,
    `SELECT id FROM events WHERE id = muid_decode($1)`, id).Scan(&got); err != nil {
    return err
}
```

Passing the raw octets as `id[:]` is the other option, and it is where the two drivers diverge:

| Parameter for a `muid` column | pgx v5                   | lib/pq, default settings         |
| ----------------------------- | ------------------------ | -------------------------------- |
| `muid_decode($1)` with `id`   | works                    | works                            |
| `$1::bytea` with `id[:]`      | works                    | works                            |
| `$1` with `id[:]`             | works                    | fails for most ids, see below    |
| `$1` with `id`                | domain `CHECK` violation | domain `CHECK` violation         |

lib/pq picks its `bytea` parameter encoding from the parameter's type OID, and the OID PostgreSQL
infers for a `muid` column is the domain's, not `bytea`'s, so with the default
`binary_parameters=no` the raw octets go out as text. Whether that survives depends on the value:
most ids trip the server's UTF-8 check — `invalid byte sequence for encoding "UTF8"`, naming
whichever octets it stopped on — while an id that is both valid UTF-8 and free of any backslash
for `bytea`'s escape format to consume is accepted and stored correctly, which makes the failure
look intermittent. The explicit `::bytea` cast puts
the OID back, and `binary_parameters=yes` on the connection string also makes a plain `$1` work.
`id[:]` further needs an addressable value — `muid.New()[:]` does not compile.

Two failures do not announce themselves:

- `WHERE id = $1` with a `Muid` against a `muid` column raises nothing and matches nothing. The
  text is read as `bytea` input here too, giving the same 16 octets, but a comparison resolves to
  the base type's operator instead of assigning to the domain, so the `CHECK` never runs: nothing
  rejects the value and nothing equals it. Write `WHERE id = muid_decode($1)`.
- A column typed `bytea` rather than `muid` accepts that same direct bind and stores the 16 ASCII
  octets of the text form — `9ys9FwBMWJ822k35` lands as `\x397973394677424d574a3832326b3335`,
  with no error anywhere. The domain's `CHECK` is what turns that into a rejected row.

pgx v5's native interface (`pgx.Conn`, `pgxpool`) supports `driver.Valuer` and `sql.Scanner` too,
through its `pgtype.Map`, and behaved the same as `stdlib` in every case above.

## Function reference

| Function                | Returns       | Volatility                        | Median cost      |
| ----------------------- | ------------- | --------------------------------- | ---------------- |
| `muid_new()`            | `bytea`       | volatile                          | 4.3 µs           |
| `muid_new_text()`       | `text`        | volatile                          | 19 µs            |
| `muid_new_pgcrypto()`   | `bytea`       | volatile                          | 4.3 µs           |
| `muid_encode(bytea)`    | `text`        | immutable, strict, parallel safe  | 15.3 µs          |
| `muid_decode(text)`     | `bytea`       | immutable, strict, parallel safe  | 15.0 µs          |
| `muid_is_valid(bytea)`  | `boolean`     | immutable, strict, parallel safe  | 2.1 µs           |
| `muid_is_valid(text)`   | `boolean`     | immutable, strict, parallel safe  | 16.7 µs          |
| `muid_crc16(bytea)`     | `integer`     | immutable, strict, parallel safe  | 1.65 µs          |
| `muid_time(bytea)`      | `timestamptz` | immutable, strict, parallel safe  | 3.0 µs           |

`muid_new_pgcrypto()` is `muid_new()` with `gen_random_bytes(2)` as its entropy source and
needs `CREATE EXTENSION pgcrypto`. It benchmarked identically, so it is only worth using
where pgcrypto is already the house source of randomness; the default `muid_new()` takes its
two random octets from core `gen_random_uuid()` and needs no extension.

The conversion functions raise on malformed input rather than returning something plausible.
`muid_encode` rejects anything that is not 12 octets. `muid_decode` applies every rule of
SPEC.md section 5.1 — length, alphabet, and checksum — so `muid_decode('0000000000000000')`
raises instead of handing back twelve zero octets. `muid_time` rejects an input that is not
12 octets.

The two `muid_is_valid` overloads are total by contrast: every non-NULL input either passes or
returns false, including wrong lengths, empty values and text that is not base62 at all. They check
shape first and only then touch octets, sequenced with `IF` rather than chained with `AND`,
because PostgreSQL does not promise left-to-right evaluation of `AND`.

All the pure functions are strict, so `NULL` in gives `NULL` out, which is what lets a nullable
`muid` column hold `NULL`.

`muid_crc16` computes CRC-16/CCITT-FALSE over any input length. It and `muid_decode_raw`, the
unchecked base62 conversion the validators and `muid_decode` are built on, are internal
plumbing: call `muid_decode` and the validators instead.

## Performance

Medians over 100 000 generated ids on PostgreSQL 18.4 (`postgres:18-alpine`) in a Linux x86_64
container on a macOS host with an Intel Core i9-9900K. Treat them as indicative: they were
measured on one machine with a default configuration, and they place the operations relative
to each other rather than predicting your throughput.

| Operation                                 | Median   |
| ----------------------------------------- | -------- |
| `gen_random_uuid()`, for reference        | 0.81 µs  |
| `muid_crc16`, lookup table (shipped)      | 1.65 µs  |
| `muid_is_valid(bytea)`                    | 2.1 µs   |
| `muid_time`                               | 3.0 µs   |
| `muid_new()`                              | 4.3 µs   |
| `muid_crc16`, bit loop (rejected variant) | 7.67 µs  |
| `muid_decode`, checksum included          | 15.0 µs  |
| `muid_encode`                             | 15.3 µs  |
| `muid_is_valid(text)`                     | 16.7 µs  |
| `muid_new_text()`                         | 19 µs    |

Two results shaped the implementation:

- The checksum is table-driven. A 256-entry lookup table held as a 512-byte `bytea` constant
  beat the reference bit loop by 4.7x: the bit loop iterates once per bit, the table once per
  octet.
- The base62 codec uses `numeric` with `div()` and `mod()`, 16 iterations. Two hand-rolled
  `bigint` variants that avoid `numeric` arithmetic were no faster on the same inputs: 32-bit
  limbs landed within a few percent on encode and about a tenth behind on decode, byte limbs
  about four times behind on both. PL/pgSQL statement overhead dominates the arithmetic, so
  the variant with the fewest loop iterations wins even though its arithmetic type is the
  slowest of the three.

The codec must use `div()` and `mod()` rather than `/` and `trunc()`: `numeric` division
rounds at PostgreSQL's internal precision, which makes `trunc(value / 62)` off by one for some
96-bit operands and silently corrupts the encoding.

In practice a generated id costs a few microseconds against 0.81 µs for `gen_random_uuid()`,
which is noise next to the rest of an ordinary `INSERT`. Bulk loads are where the difference
shows: 100 000 rows took 7.6 µs/row into a `muid` column and 41.0 µs/row into a `muid_text`
one. For loads large enough that this matters, generate the ids in the application and insert
them as data.

## Semantics and caveats

**Microsecond clock.** `clock_timestamp()` resolves to microseconds, so ids generated here
always have three zero digits at the bottom of the nanosecond field. That is valid: SPEC.md
makes nanoseconds the storage unit, not a promise about clock resolution. It also means up to
a microsecond of ids share one timestamp and are ordered only by their random fields.

**No monotonic generator state.** SPEC.md section 6 defines a generator as serialized state —
`last` and `rnd` — that guarantees strict monotonicity and uniqueness within one generator.
`muid_new()` implements none of it: each call reads the clock and draws two fresh random
octets, with no shared state and no lock. The consequences are worth stating plainly:

- Uniqueness is probabilistic, not guaranteed. Two calls landing in the same microsecond
  collide with probability 2^-16, the same kind of guarantee a random UUID gives and weaker
  than the Go implementation's.
- Ordering is only as strict as the clock. Ids generated in the same microsecond, whether by
  concurrent backends or by one loop running faster than a microsecond, are ordered by their
  random fields, so a later call can sort before an earlier one.
- A backward clock jump is not absorbed. Without `last` there is nothing to hold the line, so
  ids follow the clock wherever it goes.

Making the generator strictly monotonic requires shared state that survives across backends:
an advisory lock plus a table row, or a sequence used as the counter behind the random field.
That is a possible extension, and it would cost a round trip to that shared state on every
call.

**`muid_time()` keeps microseconds, not nanoseconds.** It returns `timestamptz`, whose
resolution is a microsecond, so the last three digits of the nanosecond field are lost — for
ids generated here they are zero anyway, but an id from the Go implementation carries
nanoseconds that this function cannot return. Read the first eight octets yourself when you
need them. It does cover the whole timestamp range SPEC.md section 2.6 allows: the octets are
accumulated in `numeric` rather than a signed `bigint`, so timestamps at or above 2^63 —
valid, and reachable in the widened format — come back as the far-future instants they are
rather than as pre-epoch ones.

**pgcrypto is optional.** Nothing in the file requires it; `muid_new_pgcrypto()` is the only
function that references `gen_random_bytes`, and creating it without the extension present is
harmless since PL/pgSQL resolves the call at execution time.

## Future work

A C extension would raise the ceiling this implementation runs into. A real base type with its
own input and output functions would store the raw 12 octets without `bytea`'s length header,
print and accept the 16-character text without explicit `muid_encode`/`muid_decode` calls,
carry its own operator class, and run the codec at C speed instead of a few microseconds per
call. `pgx_ulid` is prior art for exactly that shape. The price is a build per platform and
version, an install that most managed PostgreSQL providers will not permit, and a maintenance
burden this file does not have.

The strictly monotonic generator described above is the other open item, and it does not need
C — only shared state.
