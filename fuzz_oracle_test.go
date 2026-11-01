//go:build oracle

package acars

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/avanha/golibacars/internal/oracle"
)

func compareWithLibacars(t *testing.T, label, text string, dir Dir) bool {
	t.Helper()
	wj, wt, wok := oracle.Decode(label, text, oracle.Dir(dir))
	r := Decode(label, text, dir)
	var gj, gt string
	if r != nil {
		gj, gt = r.JSON(), r.Text()
	}
	if (r != nil) == wok && gj == wj && gt == wt {
		return true
	}
	t.Errorf("%s dir %d %q\n  libacars ok=%v json=%s\n  port     ok=%v json=%s\n  libacars text=%q\n  port     text=%q",
		label, dir, text, wok, wj, r != nil, gj, wt, gt)
	return false
}

// TestFuzzCPDLC feeds random and mutated CPDLC payloads to libacars and the port and requires the same output.
// It needs libacars: go test -tags oracle. GOACARS_FUZZ sets the number of cases (default 200000);
// GOACARS_GOLDEN, if set, supplies real payloads to mutate.
func TestFuzzCPDLC(t *testing.T) {
	n := 200000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		var v int
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(1))
	var seeds [][]byte
	if path := os.Getenv("GOACARS_GOLDEN"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var c goldenCase
			json.Unmarshal(sc.Bytes(), &c)
			if i := strings.Index(c.Text, ".AT1."); i >= 0 && len(c.Text) > i+12 {
				if b, err := hex.DecodeString(strings.TrimRight(c.Text[i+12:], "|\r\n ")); err == nil && len(b) > 2 {
					seeds = append(seeds, b[:len(b)-2])
				}
			}
		}
	}
	decoded, failures := 0, 0
	for i := 0; i < n && failures < 5; i++ {
		var payload []byte
		if len(seeds) > 0 && rng.Intn(2) == 0 {
			payload = append([]byte(nil), seeds[rng.Intn(len(seeds))]...)
			for k := rng.Intn(4); k >= 0; k-- {
				payload[rng.Intn(len(payload))] ^= 1 << uint(rng.Intn(8))
			}
			if rng.Intn(5) == 0 {
				payload = payload[:1+rng.Intn(len(payload))]
			}
		} else {
			payload = make([]byte, 1+rng.Intn(40))
			rng.Read(payload)
		}
		// random bytes plus a fake CRC: the CRC is not checked before decoding
		full := append(payload, byte(rng.Intn(256)), byte(rng.Intn(256)))
		dir := Dir(1 + rng.Intn(2))
		if !compareWithLibacars(t, "BA", cpdlcText(full), dir) {
			failures++
		}
		if r := Decode("BA", cpdlcText(full), dir); r != nil && !strings.Contains(r.JSON(), `"err":true`) {
			decoded++
		}
	}
	t.Logf("%d cases, %d decoded without error", n, decoded)
}
