//go:build oracle

package acars

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestFuzzMIAM compares MIAM frames of every kind, good and damaged, with libacars.
func TestFuzzMIAM(t *testing.T) {
	n := 200000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(5))
	failures, decoded, withData := 0, 0, 0
	for i := 0; i < n && failures < 5; i++ {
		label, text, dir := randomMIAM(rng)
		if !compareWithLibacars(t, label, text, dir) {
			failures++
		}
		if r := Decode(label, text, dir); r != nil {
			decoded++
			if strings.Contains(r.JSON(), `"message":{"text"`) {
				withData++
			}
		}
	}
	t.Logf("%d cases, %d decoded, %d with a text payload", n, decoded, withData)
}
