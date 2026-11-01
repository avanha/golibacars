//go:build oracle

package acars

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestFuzzOHMA compares OHMA messages, good and damaged in many ways, with libacars.
func TestFuzzOHMA(t *testing.T) {
	n := 200000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(6))
	failures, decoded, ok := 0, 0, 0
	for i := 0; i < n && failures < 5; i++ {
		text, dir := randomOHMA(rng)
		if !compareWithLibacars(t, "H1", text, dir) {
			failures++
		}
		if r := Decode("H1", text, dir); r != nil {
			decoded++
			if strings.Contains(r.JSON(), `"err":0`) {
				ok++
			}
		}
	}
	t.Logf("%d cases, %d recognised, %d decoded without error", n, decoded, ok)
}
