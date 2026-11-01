# The port needs only Go. libacars is built here only to compare against.
LIBACARS_PC := $(CURDIR)/third_party/install/lib/pkgconfig

.PHONY: test libacars oracle-test fuzz golden regen clean-libacars

# Unit tests and the committed golden file; no libacars needed.
test:
	go vet ./... && go test ./...

# Download and build libacars 2.2.1 into third_party/ (see scripts/build-libacars.sh for what it needs).
libacars:
	scripts/build-libacars.sh

# Everything above plus the differential tests against the real libacars.
# GOACARS_GOLDEN=file adds a golden file made from real messages; GOACARS_FUZZ=n sets the number of random cases.
oracle-test:
	PKG_CONFIG_PATH=$(LIBACARS_PC) go vet -tags oracle ./... && PKG_CONFIG_PATH=$(LIBACARS_PC) go test -tags oracle ./...

# A longer random comparison: make fuzz N=2000000
N ?= 1000000
fuzz:
	PKG_CONFIG_PATH=$(LIBACARS_PC) GOACARS_FUZZ=$(N) go test -tags oracle -run 'Fuzz|RandomValid' -v .

# Rewrite testdata/golden.jsonl from libacars (after changing the generators in gen_test.go).
golden:
	PKG_CONFIG_PATH=$(LIBACARS_PC) GOACARS_WRITE_GOLDEN=1 go test -tags oracle -run WriteGolden -v .

# Regenerate fans/types_gen.go and fans/formats_gen.go from the libacars sources in third_party/.
regen:
	go run ./cmd/gen third_party/libacars/libacars fans

clean-libacars:
	rm -rf third_party
