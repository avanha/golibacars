# golibacars

A pure-Go decoder for the application layers that ride in ACARS messages: FANS-1/A CPDLC, ADS-C, MIAM, OHMA
and media advisories. It is a port of [libacars](https://github.com/szpajder/libacars) and prints exactly what
libacars prints, JSON and text, byte for byte. No cgo, no C libraries.

It follows **libacars 2.2.1** (commit `9af09a0`, 2 November 2025), built with zlib, libxml2 and jansson.

```go
r := acars.Decode("BA", "/BOMCAYA.AT1..VT-ANQ...", acars.Downlink) // label, ACARS text, direction
if r != nil {
	fmt.Println(r.JSON()) // {"arinc622":{"msg_type":"fans1a_cpdlc_msg", ... "cpdlc":{...}}}
	fmt.Println(r.Text()) // FANS-1/A CPDLC Message: ...
}
```

`Decode(label, text, dir)` is `la_acars_decode_apps`: pass the ACARS label, the message text with the
message-number and flight-ID prefix already removed, and the direction. It returns nil when nothing in the
text is recognised.

## Status

| Layer | State |
| --- | --- |
| ARINC 622 wrapper (`.AT1 .CR1 .CC1 .DR1 .ADS .DIS`, ground address, registration, CRC) | ported |
| FANS-1/A CPDLC, uplink and downlink, all 312 message elements | ported |
| ADS-C, uplink and downlink, all tags | ported |
| Media Advisory (label `SA`) | ported |
| MIAM frames and MIAM CORE v1 and v2 PDUs (labels `MA`, `H1`), deflate and CRCs included | ported |
| MIAM file-transfer reassembly | not ported: `Decode` handles one message at a time, as libacars does without a reassembly context |
| MIAM XML reformatting | not ported: it is an output option of libacars, off by default |
| OHMA (label `H1`), including its JSON envelope | ported; multipart reassembly is not (see MIAM) |
| ACARS block parsing | not planned: libacars' `la_acars_decode_apps` does not do it |

Every decoder `la_acars_decode_apps` has is ported.

## How it is checked

libacars is the reference. The tests run both and require identical output:

- **Real traffic.** 31,408 decoder inputs made from 11,730 messages in a FullFlight database (each tried in both
  directions, and with the trailing `|` that FullFlight adds to bodiless MIAM frames) give the same JSON and the
  same text. They include 3,099 CPDLC, 5,607 ADS-C, 1,122 media advisory and 3,662 MIAM results, among them 218 MIAM
  messages whose deflate data is cut short, and 17,918 inputs that libacars declines and the port declines too.
- **Random valid CPDLC.** A test encoder (`per/pertest`) writes random valid PDUs from the type tables, so every
  one of the 312 message elements is reached. 100,000 of them, both directions, match libacars.
- **Random and mutated payloads.** 200,000 random or bit-flipped CPDLC payloads, which mostly fail to decode,
  fail the same way.
- **ADS-C.** 200,000 random well-formed messages (all tags, both directions, `ADS` and `DIS`) and 300,000 with
  random bytes match.
- **MIAM.** 2,000,000 generated frames of every kind (single transfer, file transfer request, accept, segment,
  abort, pause, resume), with v1 and v2 PDUs, every padding value, deflated, stored and raw bodies, good and bad CRCs,
  and truncated or bit-flipped compressed data, match.
- **OHMA.** 400,000 generated messages (prefixes, the `OHMA` and `RYKO` markers, repeated first fragments, Base64
  damage, bad compression, and JSON envelopes with wrong types, missing fields, bad escapes, bad UTF-8, huge numbers
  and deep nesting) match. The stored FullFlight messages contain no OHMA, so this decoder has not been checked
  against real traffic.

`testdata/golden.jsonl` holds 540 synthetic cases with libacars' output, so `go test ./...` runs the comparison
without libacars. The differential tests need libacars (see below) and a build tag.

```
make test            # unit tests and the committed golden file; needs only Go
```

The differential tests run the real libacars, so they need it built. `make libacars` downloads libacars 2.2.1
(the commit the port follows) into `third_party/` and builds it there; nothing is installed system-wide.
It needs git, cmake, pkg-config and the development files for jansson, zlib and libxml2:

```
# Debian / Ubuntu
sudo apt install build-essential git cmake pkg-config libjansson-dev zlib1g-dev libxml2-dev
# macOS
brew install cmake pkg-config jansson zlib libxml2

make libacars        # once
make oracle-test     # all tests, including the random comparisons with libacars (about a minute)
make fuzz N=2000000  # a longer random run
```

A missing `-dev` package does not stop the libacars build, it silently leaves a feature out, which would make
the comparison meaningless; the script checks that zlib and jansson were picked up and fails if not.

To compare with a golden file made from your own messages (kept out of git):

```
# messages.jsonl: one {"label": "MA", "dir": 2, "text": "..."} per line; dir 1 is uplink, 2 downlink
PKG_CONFIG_PATH=$PWD/third_party/install/lib/pkgconfig \
  go run -tags oracle ./internal/oracle/cmd/dump messages.jsonl > golden.jsonl
GOACARS_GOLDEN=golden.jsonl go test -run Golden .
```

`make golden` rewrites `testdata/golden.jsonl` after the generators in `gen_test.go` change, and `make regen`
rebuilds the FANS-1/A tables from the libacars sources in `third_party/`.

## How it works

- `per` decodes unaligned PER (X.691) from type tables. It ports the decoding half of asn1c 0.9.28, the compiler
  that made libacars' FANS-1/A code, and follows it where it differs from the standard.
- libacars ships the *generated* C for the FANS-1/A types but not the ASN.1 source. `cmd/gen` reads that C (the
  type descriptors, the constraints and the formatter tables) and writes `fans/types_gen.go` and
  `fans/formats_gen.go`. The output is checked in. To refresh it from a newer libacars: change the commit in `scripts/build-libacars.sh`, then `make libacars regen`.
- `ohma` has a JSON parser that follows jansson's rules (the library libacars uses), since Go's `encoding/json`
  accepts things jansson refuses, such as invalid UTF-8 and `\u0000`.
- `miam` has its own raw-deflate decoder (`inflate.go`, after zlib's `puff.c`). libacars keeps whatever zlib
  produced before the data ran out or went bad, and Go's `compress/flate` stops one symbol earlier, which showed
  up as a one-character difference on 218 real messages.
- `fans`, `adsc`, `mediaadv`, `miam` and `arinc` port the hand-written parts and formatters, quirks included (for
  example, an ADS-C contract request with no request groups leaves the text indent one step deeper, as libacars
  does).

## Known differences

- libacars asserts on an unknown direction for CPDLC and ADS-C, and its ADS-C table choice is static state. Pass
  `Uplink` or `Downlink`.
- When an AT1-family message has fewer than two bytes of hex after the registration, libacars subtracts the CRC
  length from an unsigned count and reads past the buffer. The port reports an unparseable message, which is
  what libacars happens to print.
- libacars' Base85 decoder reads characters as `char`; the port treats them as signed, as on x86 and Apple
  silicon. On platforms where `char` is unsigned, libacars gives different values for non-ASCII bytes in MIAM text.
- Output is compared as bytes of the C `la_vstring`, so strings with embedded NULs are compared in full.

## Licence

MIT, as libacars (https://github.com/szpajder/libacars). See `LICENSE` and `NOTICE`.
