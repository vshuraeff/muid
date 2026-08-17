-- µID for PostgreSQL: encode, decode, validate and generate identifiers in pure SQL/PL/pgSQL.
-- No extension is required. The normative format is SPEC.md in this repository; POSTGRES.md
-- documents this file, its performance and its deviations from SPEC.md section 6.
--
-- Load it with:  psql -d yourdb -f postgres/muid.sql
--
-- Verified on PostgreSQL 18.4. The only version-sensitive dependency is gen_random_uuid(),
-- which is in core since PostgreSQL 13; pgcrypto is optional and only used by
-- muid_new_pgcrypto().

-- ============================================================
-- 1. CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF, no reflection, xorout 0)
-- ============================================================

-- 256-entry lookup table, 512 bytes, 2 big-endian bytes per entry. Generated offline from the
-- reference bit-loop of SPEC.md section 3.2 and checked against crc16("123456789") = 0x29B1.
-- Table-driven is ~4.7x faster here than running the bit loop in PL/pgSQL.
CREATE OR REPLACE FUNCTION muid_crc16(data bytea)
RETURNS integer
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  tbl CONSTANT bytea := decode(
    '0000102120423063408450a560c670e781089129a14ab16bc18cd1ade1cef1ef123102103273225252b5429472f762d693398318b37ba35ad3bdc39cf3ffe3de246234430420140164e674c744a45485a56ab54b85289509e5eef5cfc5acd58d365326721611063076d766f6569546b4b75ba77a97198738f7dfe7fed79dc7bc48c458e5688678a70840186128023823c9ccd9ede98ef9af89489969a90ab92b5af54ad47ab76a961a710a503a332a12dbfdcbdcfbbfeb9e9b798b58bb3bab1a6ca67c874ce45cc52c223c030c601c41edaefd8fcdecddcdad2abd0b8d689d497e976eb65ed54ef43e132e321e510e70ff9fefbedfddcffcbf1baf3a9f598f78918881a9b1caa1ebd10cc12df14ee16f108000a130c220e3500440257046606783b99398a3fbb3dac33dd31ce37ff35e02b1129022f332d24235521462777256b5eaa5cb95a88589f56ee54fd52cc50d34e224c314a004817466644754244405a7dbb7fa879997b8e75ff77ec71dd73c26d336f2069116b06657767646155634d94cc96df90ee92f99c889e9b98aa9ab584448657806682718c008e1388228a3cb7ddb5ceb3ffb1e8bf99bd8abbbbb9a4a755a546a377a160af11ad02ab33a92fd2eed0fdd6ccd4dbdaaad8b9de88dc97c266c075c644c453ca22c831ce00cc1ef1fff3ecf5ddf7caf9bbfba8fd99ff86e177e364e555e742e933eb20ed11ef0',
    'hex');
  crc integer := 65535; -- 0xFFFF
  i integer;
  idx integer;
BEGIN
  FOR i IN 0 .. length(data) - 1 LOOP
    idx := ((crc >> 8) # get_byte(data, i)) & 255;
    crc := ((crc << 8) & 65535) # ((get_byte(tbl, idx * 2) << 8) | get_byte(tbl, idx * 2 + 1));
  END LOOP;
  RETURN crc;
END;
$$;

-- ============================================================
-- 2. base62 encode/decode (SPEC.md sections 4.2 and 4.3)
-- ============================================================

CREATE OR REPLACE FUNCTION muid_encode(data bytea)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  alphabet CONSTANT text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
  value numeric := 0;
  i integer;
  d integer;
  chars text[];
BEGIN
  IF length(data) <> 12 THEN
    RAISE EXCEPTION 'muid: binary length must be 12, got %', length(data);
  END IF;
  FOR i IN 0 .. 11 LOOP
    value := value * 256 + get_byte(data, i);
  END LOOP;
  -- numeric division rounds at PostgreSQL's internal precision, so trunc(value/62) can be
  -- off by one for large operands; div()/mod() are exact integer truncating division.
  FOR i IN REVERSE 16 .. 1 LOOP
    d := mod(value, 62)::integer;
    chars[i] := substr(alphabet, d + 1, 1);
    value := div(value, 62);
  END LOOP;
  RETURN array_to_string(chars, '');
END;
$$;

-- internal: the base62 to octets conversion of SPEC.md 4.3 rules 1 and 2 only. It enforces
-- length and alphabet but not the checksum, so callers must validate. `s` is declared
-- COLLATE "C" because strpos() rejects a nondeterministic collation.
CREATE OR REPLACE FUNCTION muid_decode_raw(text_in text)
RETURNS bytea
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  alphabet CONSTANT text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
  s text COLLATE "C" := text_in;
  value numeric := 0;
  i integer;
  d integer;
  result bytea := decode('000000000000000000000000', 'hex');
BEGIN
  IF length(s) <> 16 THEN
    RAISE EXCEPTION 'muid: text length must be 16, got %', length(s);
  END IF;
  FOR i IN 1 .. 16 LOOP
    d := strpos(alphabet, substr(s, i, 1)) - 1;
    IF d < 0 THEN
      RAISE EXCEPTION 'muid: invalid character % at position %', substr(s, i, 1), i;
    END IF;
    value := value * 62 + d;
  END LOOP;
  FOR i IN REVERSE 11 .. 0 LOOP
    result := set_byte(result, i, mod(value, 256)::integer);
    value := div(value, 256);
  END LOOP;
  RETURN result;
END;
$$;

-- public decode: every rule of SPEC.md 5.1, checksum included, so a mistyped or corrupted id
-- raises instead of returning plausible octets. No range rule is needed: every 16-character
-- base62 string is below the section 2.6 bound by construction.
CREATE OR REPLACE FUNCTION muid_decode(text_in text)
RETURNS bytea
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  result bytea;
BEGIN
  result := muid_decode_raw(text_in);
  IF muid_crc16(substring(result FROM 1 FOR 10))
     <> ((get_byte(result, 10) << 8) | get_byte(result, 11)) THEN
    RAISE EXCEPTION 'muid: checksum mismatch in %', text_in;
  END IF;
  RETURN result;
END;
$$;

-- ============================================================
-- 3. validation (SPEC.md section 5)
-- ============================================================

-- binary: length, then the section 2.6 bound via bytea's octet-wise comparison, then the CRC.
-- The steps are sequenced with IF rather than chained with AND: PostgreSQL does not promise
-- left-to-right evaluation of AND, and get_byte() raises on an input shorter than 12 octets.
CREATE OR REPLACE FUNCTION muid_is_valid(data bytea)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
BEGIN
  IF length(data) <> 12 THEN
    RETURN false;
  END IF;
  IF data >= '\x9a09afbae83050a9de010000'::bytea THEN
    RETURN false;
  END IF;
  RETURN muid_crc16(substring(data FROM 1 FOR 10))
         = ((get_byte(data, 10) << 8) | get_byte(data, 11));
END;
$$;

-- text: length and alphabet via regex (no range rule needed, SPEC.md 4.3/5.1), then the CRC
-- over the decoded octets -- the only way to check a checksum defined over the binary form.
-- Sequenced with IF for the same reason as above, and matched under COLLATE "C" so that a
-- nondeterministic database collation cannot make the pattern match raise.
CREATE OR REPLACE FUNCTION muid_is_valid(text_in text)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  s text COLLATE "C" := text_in;
  d bytea;
BEGIN
  IF s !~ '^[0-9A-Za-z]{16}$' THEN
    RETURN false;
  END IF;
  d := muid_decode_raw(s);
  RETURN muid_crc16(substring(d FROM 1 FOR 10))
         = ((get_byte(d, 10) << 8) | get_byte(d, 11));
END;
$$;

-- ============================================================
-- 4. generation (SPEC.md section 6, partial -- see POSTGRES.md)
-- ============================================================

-- default entropy source: gen_random_uuid(), core since PostgreSQL 13, no extension needed.
CREATE OR REPLACE FUNCTION muid_new()
RETURNS bytea
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
  ts_ns bigint;
  rnd_bytes bytea;
  crc integer;
  result bytea;
BEGIN
  -- clock_timestamp() has microsecond resolution, so the low 3 nanosecond digits are always
  -- zero. extract(epoch ...) returns exact numeric (not float8), so this multiply is exact.
  ts_ns := (extract(epoch FROM clock_timestamp()) * 1000000000)::bigint;
  rnd_bytes := substring(uuid_send(gen_random_uuid()) FROM 1 FOR 2);
  result := overlay(decode('000000000000000000000000', 'hex') PLACING int8send(ts_ns) FROM 1 FOR 8);
  result := set_byte(result, 8, get_byte(rnd_bytes, 0));
  result := set_byte(result, 9, get_byte(rnd_bytes, 1));
  crc := muid_crc16(substring(result FROM 1 FOR 10));
  result := set_byte(result, 10, (crc >> 8) & 255);
  result := set_byte(result, 11, crc & 255);
  RETURN result;
END;
$$;

-- alternate entropy source, requires: CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- benchmarked equal to muid_new(), so it is only worth using where pgcrypto's CSPRNG is
-- already the house entropy source.
CREATE OR REPLACE FUNCTION muid_new_pgcrypto()
RETURNS bytea
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
  ts_ns bigint;
  rnd_bytes bytea;
  crc integer;
  result bytea;
BEGIN
  ts_ns := (extract(epoch FROM clock_timestamp()) * 1000000000)::bigint;
  rnd_bytes := gen_random_bytes(2);
  result := overlay(decode('000000000000000000000000', 'hex') PLACING int8send(ts_ns) FROM 1 FOR 8);
  result := set_byte(result, 8, get_byte(rnd_bytes, 0));
  result := set_byte(result, 9, get_byte(rnd_bytes, 1));
  crc := muid_crc16(substring(result FROM 1 FOR 10));
  result := set_byte(result, 10, (crc >> 8) & 255);
  result := set_byte(result, 11, crc & 255);
  RETURN result;
END;
$$;

CREATE OR REPLACE FUNCTION muid_new_text()
RETURNS text
LANGUAGE sql
VOLATILE
AS $$
  SELECT muid_encode(muid_new());
$$;

-- convenience: the timestamp field as a timestamptz. The octets are accumulated in numeric
-- because the field is unsigned (SPEC.md 2.3): a bigint reassembly turns every timestamp at
-- or above 2^63 into a negative, pre-epoch instant. Whole and fractional seconds are added
-- separately so the fraction keeps its precision; timestamptz then holds microseconds, which
-- is all the resolution this function preserves.
CREATE OR REPLACE FUNCTION muid_time(data bytea)
RETURNS timestamptz
LANGUAGE plpgsql
IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
  ns numeric := 0;
  i integer;
BEGIN
  IF length(data) <> 12 THEN
    RAISE EXCEPTION 'muid: binary length must be 12, got %', length(data);
  END IF;
  FOR i IN 0 .. 7 LOOP
    ns := ns * 256 + get_byte(data, i);
  END LOOP;
  RETURN to_timestamp(div(ns, 1000000000)::double precision)
         + make_interval(secs => (mod(ns, 1000000000) / 1000000000.0)::double precision);
END;
$$;

-- ============================================================
-- 5. domains
-- ============================================================

-- the drops are deliberately not CASCADE: re-loading this file into a database whose tables
-- already use the domains fails here instead of dropping those columns.
DROP DOMAIN IF EXISTS muid;
CREATE DOMAIN muid AS bytea
  CHECK (muid_is_valid(VALUE));
COMMENT ON DOMAIN muid IS 'µID, 12-byte binary form (SPEC.md). Preferred storage form: compact, no collation dependency, cheap CHECK.';

DROP DOMAIN IF EXISTS muid_text;
CREATE DOMAIN muid_text AS text COLLATE "C"
  CHECK (muid_is_valid(VALUE));
COMMENT ON DOMAIN muid_text IS 'µID, 16-char canonical text form (SPEC.md). Requires COLLATE "C" so byte order matches base62 numeric order (SPEC.md section 7).';
