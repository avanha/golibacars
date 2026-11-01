# Working on golibacars with an LLM agent

golibacars is a pure-Go port of [libacars](https://github.com/szpajder/libacars) (module
`github.com/avanha/golibacars`, package `acars`). It decodes the application layers in ACARS messages (FANS-1/A CPDLC,
ADS-C, MIAM, OHMA, media advisories) and must print **exactly** what libacars prints, JSON and text, byte for byte.
Read `README.md` first (status, how it is checked, known differences). It follows libacars 2.2.1, commit `9af09a0`.

## The rule that decides everything

**libacars is the reference, not the standard and not common sense.** If the port and libacars differ, the port is wrong,
even where libacars looks buggy. Port its quirks (the shared indent state in the ADS-C formatter, the Base85 `z` rule,
a CR/LF-stripped Base64 length, reads of the byte before a buffer) and list the ones that cannot be matched under
"Known differences" in the README. Never "fix" output to be nicer. A change is done when `make oracle-test` passes.

## Layout

- `acars.go` is `Decode(label, text, dir)`, the port of `la_acars_decode_apps`: the label picks which decoders run, in libacars' order.
- `internal/proto` is the chain of decoded layers (outermost first) and how it renders; `internal/ljson` writes JSON the
  way libacars' `json.c` does (compact, trailing commas trimmed, raw bytes escaped); `internal/crc` has the CRCs MIAM uses.
- `arinc/` is the ARINC 622 wrapper (`.AT1 .ADS ...`, CRC) and hands the payload to `fans` or `adsc`.
- `per/` is the unaligned-PER decoder driven by type tables (a port of asn1c's); `per/pertest` is a test-only random
  encoder. `fans/` is FANS-1/A CPDLC: `types_gen.go` and `formats_gen.go` are **generated**, `fans.go` and `format.go` are not.
- `adsc/`, `mediaadv/`, `miam/` (including its own zlib-faithful `inflate.go`) and `ohma/` (including a jansson-compatible JSON parser).
- `cmd/gen` makes the generated files from libacars' C. `internal/oracle` runs the real libacars through cgo (build tag `oracle`).
- Tests: `golden_test.go` and `testdata/golden.jsonl` (synthetic cases with libacars' output, no cgo), and the
  `*_oracle_test.go` files (random comparisons with the live libacars; `gen_test.go` has the generators).

## Build and test

    make test           # vet, unit tests and the golden file; needs only Go
    make libacars       # once: downloads and builds libacars at the pinned commit into third_party/
    make oracle-test    # everything, including the random comparisons with libacars
    make fuzz N=2000000 # longer random run

Run `make test` after any change and `make oracle-test` after any change to a decoder or formatter. If libacars is not built, say
that the comparison was not run rather than claiming the change is verified. `go test` must keep passing with `CGO_ENABLED=0`.

## Rules

- **Do not edit `fans/types_gen.go` or `fans/formats_gen.go`.** Change `cmd/gen` and run `make regen`. The generator fails
  loudly (it asserts) on anything it does not understand, such as PER extension markers; keep it that way.
- **No real traffic in git.** `testdata/golden.jsonl` is made only from the generators (`make golden`). Golden files made from real
  messages stay local (`GOACARS_GOLDEN=...`). Do not commit corpora, databases or captured messages.
- **Add a generator case for every new behaviour**, so the random comparison reaches it, and keep the generators valid enough
  that a good share of cases decode without error (the tests log the rate; a generator that only produces rejects tests nothing).
- A failing comparison prints both outputs. Read the first difference; fix the cause, not the symptom. Typical causes:
  indent copied by value in C but shared in Go, a NUL-terminated C string versus a Go string, signed `char`, an unsigned
  length that underflows.
- Style: match the surrounding code, run `gofmt`, comments say why. Keep the README in step (status table, test counts, known differences).
- Commit, tag and push only when asked. Releases are annotated `v0.x.y` tags with the libacars version in the message
  ("follows libacars 2.2.1"). Never use libacars' own version number as the tag: `v2.x` would force a `/v2` module path.

## Syncing with a new libacars release

Do this when upstream releases (watch https://github.com/szpajder/libacars/releases) or a decoder here disagrees with a newer libacars.

1. **Read what changed.** In `third_party/libacars`: `git fetch --tags origin`, read `CHANGELOG.md`, then
   `git diff 9af09a0..<new-tag> --stat -- libacars/ CMakeLists.txt` (use the commit in `scripts/build-libacars.sh` as the old side).
   Map each changed file to its Go home:

   | libacars | here |
   | --- | --- |
   | `acars.c` (`la_acars_apps_parse_and_reassemble`) | `acars.go` |
   | `arinc.c`, `crc.c` | `arinc/`, `internal/crc` |
   | `cpdlc.c`, `asn1-format-cpdlc-*.c`, `asn1/FANS*.c` | `fans/` (mostly regenerated) |
   | `asn1/per_*.c`, `asn1/constr_*.c`, `asn1/*String.c`, `INTEGER.c`, `NativeEnumerated.c` | `per/` |
   | `adsc.c` | `adsc/` |
   | `media-adv.c` | `mediaadv/` |
   | `miam.c`, `miam-core.c`, `util.c` (inflate, hexdump, multiline text), `crc.c` | `miam/`, `internal/crc` |
   | `ohma.c`, `util.c` (Base64), jansson behaviour | `ohma/` |
   | `json.c`, `vstring.*` | `internal/ljson`, `internal/proto` |
   | `libacars.c` (tree formatting), `macros.h` | `internal/proto` |

   A **new `.c` file in `CMakeLists.txt`, or a new branch in `la_acars_apps_parse_and_reassemble`, is a new decoder**: it needs a new package.
2. **Move the pin.** Put the new commit in `scripts/build-libacars.sh`, then `make clean-libacars libacars`. It refuses to continue
   if zlib or jansson were left out. Check `third_party/libacars/build/libacars/config.h` for new `WITH_*` options and any
   new `la_config` settings whose defaults change output.
3. **Regenerate.** `make regen` and look at `git diff fans/`. New message elements or changed labels show up here. If `cmd/gen` stops
   with an assertion, libacars changed how asn1c output looks (a newer asn1c, extensible types): extend the generator and the
   decoder in `per/` (the C sources in `asn1/` are the specification), do not loosen the assertion.
4. **Run the comparison before porting anything.** `make oracle-test`. Failures tell you what to port. Also run any local real-traffic
   golden file (`GOACARS_GOLDEN=...`); real traffic finds things the generators do not.
5. **Port each changed function.** Read the C, port it with its quirks, add generator cases for the new paths, rerun until
   `make oracle-test` passes. Larger runs (`make fuzz N=2000000`) before calling it done.
6. **Finish.** `make golden` to refresh `testdata/golden.jsonl`; update the version and commit in `README.md` and `NOTICE`, the
   test counts in the README, and "Known differences" if one appeared or went away; then ask whether to commit and tag
   (version rules are in the README under "Versions": moving to a newer libacars is a minor bump).

If upstream starts doing something this port deliberately does not (reassembly with a context, XML reformatting, JSON
pretty-printing, all off by default), leave it out and keep the README's status table honest.
