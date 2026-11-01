//go:build oracle

package acars

import (
	"encoding/hex"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestFuzzADSC feeds random ADS-C payloads, built mostly from valid tag numbers, to libacars and the port.
func TestFuzzADSC(t *testing.T) {
	n := 300000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(3))
	tags := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 255, 0, 24, 99}
	failures, ok := 0, 0
	for i := 0; i < n && failures < 5; i++ {
		var payload []byte
		for k := 1 + rng.Intn(4); k > 0; k-- {
			payload = append(payload, tags[rng.Intn(len(tags))])
			body := make([]byte, rng.Intn(20))
			rng.Read(body)
			if rng.Intn(3) == 0 && len(body) > 1 {
				body[1] &= 0x3f // keep the second octet small, as reason and group counts are
			}
			payload = append(payload, body...)
		}
		full := append(payload, byte(rng.Intn(256)), byte(rng.Intn(256)))
		imi := ".ADS"
		if rng.Intn(8) == 0 {
			imi = ".DIS"
			full = full[len(full)-3:]
		}
		text := "/BOMCAYA" + imi + "..VT-AN" + strings.ToUpper(hex.EncodeToString(full))
		dir := Dir(1 + rng.Intn(2))
		if !compareWithLibacars(t, "B6", text, dir) {
			failures++
		}
		if r := Decode("B6", text, dir); r != nil && !strings.Contains(r.JSON(), `"err":true`) {
			ok++
		}
	}
	t.Logf("%d cases, %d parsed without error", n, ok)
}

// TestFuzzADSCWellFormed builds ADS-C messages whose tags have the right lengths, so that every tag type is parsed.
func TestFuzzADSCWellFormed(t *testing.T) {
	n := 200000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(4))
	failures, parsed := 0, 0
	seen := map[string]bool{}
	for i := 0; i < n && failures < 5; i++ {
		text, dir := randomADSC(rng)
		if !compareWithLibacars(t, "B6", text, dir) {
			failures++
		}
		if r := Decode("B6", text, dir); r != nil && !strings.Contains(r.JSON(), `"err":true`) {
			parsed++
			for _, m := range tagKeyRe.FindAllStringSubmatch(r.JSON(), -1) {
				seen[m[1]] = true
			}
		}
	}
	t.Logf("%d cases, %d parsed without error, %d distinct tag types seen", n, parsed, len(seen))
}

var tagKeyRe = regexp.MustCompile(`\{"(\w+)":\{`)
