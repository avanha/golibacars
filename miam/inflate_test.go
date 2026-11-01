package miam

import (
	"bytes"
	"compress/flate"
	"math/rand"
	"testing"
)

// TestInflateRoundTrip checks the inflater on valid streams of every block type against the encoder in the standard library.
func TestInflateRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 300; i++ {
		data := make([]byte, rng.Intn(3000))
		for j := range data {
			data[j] = "abcde ,/;0123"[rng.Intn(13)]
		}
		if i%7 == 0 {
			rng.Read(data)
		}
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, []int{flate.NoCompression, flate.BestSpeed, flate.DefaultCompression, flate.BestCompression, flate.HuffmanOnly}[i%5])
		w.Write(data)
		w.Close()
		out, ok := Inflate(buf.Bytes())
		if !ok || !bytes.Equal(out, data) {
			t.Fatalf("case %d (%d bytes): ok=%v, got %d bytes", i, len(data), ok, len(out))
		}
	}
}

// TestInflateTruncated checks that a cut-off stream yields a prefix of the data and reports failure.
func TestInflateTruncated(t *testing.T) {
	data := bytes.Repeat([]byte("A350,000065,1,1,TB000000/REP053,02,04;"), 20)
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	w.Write(data)
	w.Close()
	full := buf.Bytes()
	for n := 1; n < len(full); n++ {
		out, ok := Inflate(full[:n])
		if ok || !bytes.HasPrefix(data, out) {
			t.Fatalf("prefix %d: ok=%v, %d bytes, prefix of data %v", n, ok, len(out), bytes.HasPrefix(data, out))
		}
	}
}

func TestBase85(t *testing.T) {
	// "zz" is two groups of four zero octets; the five characters "!!!!!" are one group too.
	if got := base85("z!!!!!"); !bytes.Equal(got, make([]byte, 8)) {
		t.Errorf("got %x", got)
	}
	// ASCII85 of "Man " is "9jqo^"
	if got := base85("9jqo^"); string(got) != "Man " {
		t.Errorf("got %q", got)
	}
}
