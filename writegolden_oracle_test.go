//go:build oracle

package acars

import (
	"bufio"
	"encoding/json"
	"math/rand"
	"os"
	"testing"

	"github.com/avanha/golibacars/internal/oracle"
)

// TestWriteGolden writes testdata/golden.jsonl: synthetic messages and libacars' output for them.
// The messages are made up (random but valid values), so the file holds no real traffic.
// Run it with GOACARS_WRITE_GOLDEN=1 go test -tags oracle -run WriteGolden after changing the generators.
func TestWriteGolden(t *testing.T) {
	if os.Getenv("GOACARS_WRITE_GOLDEN") == "" {
		t.Skip("set GOACARS_WRITE_GOLDEN=1 to rewrite testdata/golden.jsonl")
	}
	rng := rand.New(rand.NewSource(7))
	type in struct {
		label, text string
		dir         Dir
	}
	var cases []in
	for i := 0; i < 150; i++ {
		text, dir := randomCPDLC(rng)
		cases = append(cases, in{"BA", text, dir})
	}
	for i := 0; i < 150; i++ {
		text, dir := randomADSC(rng)
		cases = append(cases, in{"B6", text, dir})
	}
	for i := 0; i < 120; i++ {
		label, text, dir := randomMIAM(rng)
		cases = append(cases, in{label, text, dir})
	}
	for i := 0; i < 80; i++ {
		text, dir := randomOHMA(rng)
		cases = append(cases, in{"H1", text, dir})
	}
	for _, m := range []string{
		"0EV123456VS/Hello", "0LS081530V", "0EG010203VSHGC2XI/", "0EV235959XXX", "0EX000000S",
		"1EV123456VS", "0XV123456VS", "0EZ123456VS", "0EV12345", "0EV246000VS", "0EV1234AAVS", "0EV123456VQ",
		"0LI101112VSH2/two lines\r\nsecond",
	} {
		cases = append(cases, in{"SA", m, Downlink})
	}
	for _, m := range []string{
		"/BOMCAYA.AT1..VT-AN", "/BOMCAYA.AT1..VT-ANZZ", "/BOMCAYA.AT1..VT-AN0000", "/BOMCAYA.ADS..VT-AN0000",
		"/BOMCAYA.ADS..VT-AN12", "/BOMCAYA.DIS..VT-AN0102ABCD", "/BOMC.ADS..VT-AN01020304", "/XXXXXXX.XXX..VT-AN0102",
		"/BOMCAYA.CR1..VT-AN0102ABCD", "/BOMCAYA.CC1..VT-AN0102ABCD", "/BOMCAYA.DR1..VT-AN0102ABCD", "no application here",
	} {
		for _, d := range []Dir{Uplink, Downlink} {
			cases = append(cases, in{"BA", m, d})
		}
	}
	cases = append(cases, in{"Q0", "/BOMCAYA.AT1..VT-AN0102ABCD", Downlink}, in{"B", "x", Downlink}, in{"H1", "#M1BNothing", Downlink})

	f, err := os.Create("testdata/golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	for _, c := range cases {
		g := goldenCase{Label: c.label, Dir: int(c.dir), Text: c.text}
		g.JSON, g.Out, g.OK = oracle.Decode(c.label, c.text, oracle.Dir(c.dir))
		b, _ := json.Marshal(g)
		w.Write(append(b, '\n'))
	}
	t.Logf("wrote %d cases", len(cases))
}
