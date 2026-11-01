//go:build oracle

package acars

import (
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRandomValidCPDLC encodes random valid PDUs, so that every message element is reached, and requires
// the port to print them exactly as libacars does.
func TestRandomValidCPDLC(t *testing.T) {
	n := 100000
	if s := os.Getenv("GOACARS_FUZZ"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	rng := rand.New(rand.NewSource(2))
	failures, decoded := 0, 0
	seen := map[string]bool{}
	choiceRe := regexp.MustCompile(`"choice":"([dDuU]M\d+\w*)"`)
	for i := 0; i < n && failures < 5; i++ {
		text, dir := randomCPDLC(rng)
		if !compareWithLibacars(t, "BA", text, dir) {
			failures++
		}
		if r := Decode("BA", text, dir); r != nil && strings.Contains(r.JSON(), `"err":false`) {
			decoded++
			for _, m := range choiceRe.FindAllStringSubmatch(r.JSON(), -1) {
				seen[m[1]] = true
			}
		}
	}
	t.Logf("%d cases, %d decoded, %d distinct message elements seen", n, decoded, len(seen))
	if decoded < n*9/10 {
		t.Errorf("only %d of %d random valid PDUs decoded", decoded, n)
	}
}
