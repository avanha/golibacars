package acars

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// goldenCase is one line of a golden file made by internal/oracle/cmd/dump from libacars' output.
type goldenCase struct {
	Label string `json:"label"`
	Dir   int    `json:"dir"`
	Text  string `json:"text"`
	OK    bool   `json:"ok"`
	JSON  string `json:"json"`
	Out   string `json:"out"`
}

// TestGolden decodes every message of a golden file and compares the result with libacars'.
// By default the file is testdata/golden.jsonl, made of synthetic messages; GOACARS_GOLDEN names another,
// such as one made from real traffic by internal/oracle/cmd/dump.
func TestGolden(t *testing.T) {
	path := os.Getenv("GOACARS_GOLDEN")
	if path == "" {
		path = "testdata/golden.jsonl"
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var total, same, skipped, bad int
	for sc.Scan() {
		var c goldenCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if !ported(c) {
			skipped++
			continue
		}
		total++
		r := Decode(c.Label, c.Text, Dir(c.Dir))
		var gotJSON, gotText string
		if r != nil {
			gotJSON, gotText = r.JSON(), r.Text()
		}
		if (r != nil) == c.OK && gotJSON == c.JSON && gotText == c.Out {
			same++
			continue
		}
		bad++
		if bad <= 5 {
			t.Errorf("%s dir %d %q\n  want ok=%v json=%s\n  got  ok=%v json=%s\n  want text=%q\n  got  text=%q",
				c.Label, c.Dir, c.Text, c.OK, c.JSON, r != nil, gotJSON, c.Out, gotText)
		}
	}
	t.Logf("%d compared, %d identical, %d different, %d skipped (protocol not ported)", total, same, bad, skipped)
}

// ported reports whether the port handles the protocols in a case. All of libacars' decoders are ported.
func ported(goldenCase) bool { return true }
