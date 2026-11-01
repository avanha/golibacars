package per_test

import (
	"math/rand"
	"testing"

	"github.com/avanha/golibacars/per"
	"github.com/avanha/golibacars/per/pertest"
)

var (
	small = &per.Type{Name: "Small", Kind: per.Integer,
		Per: &per.Constraints{Value: per.Constraint{Flags: per.Constrained, RangeBits: 4, EffBits: 4, Lower: 10, Upper: 25}}}
	name = &per.Type{Name: "Name", Kind: per.IA5String,
		Per: &per.Constraints{
			Value: per.Constraint{Flags: per.Constrained, RangeBits: 7, EffBits: 7, Lower: 0, Upper: 127},
			Size:  per.Constraint{Flags: per.Constrained, RangeBits: 2, EffBits: 2, Lower: 1, Upper: 4},
		}}
	pair = &per.Type{Name: "Pair", Kind: per.Sequence, RootOptional: 1, Members: []per.Member{
		{Name: "n", Type: small},
		{Name: "s", Type: name, Optional: true},
	}}
)

func TestDecodeKnownValues(t *testing.T) {
	// 0 (no optional member) | 0101 (10+5=15) | pad
	v, used, err := per.Decode(pair, []byte{0b0010_1000})
	if err != nil || used != 1 {
		t.Fatalf("err=%v used=%d", err, used)
	}
	if v.Members[0].Int != 15 || v.Members[1] != nil {
		t.Errorf("got n=%d s=%v", v.Members[0].Int, v.Members[1])
	}
	// 1 | 0011 (13) | 01 (length 1+1=2) | "Hi" as 7 bits each
	bits := "1" + "0011" + "01" + "1001000" + "1101001"
	var buf []byte
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			b <<= 1
			if i+j < len(bits) && bits[i+j] == '1' {
				b |= 1
			}
		}
		buf = append(buf, b)
	}
	v, used, err = per.Decode(pair, buf)
	if err != nil || used != len(buf) {
		t.Fatalf("err=%v used=%d of %d", err, used, len(buf))
	}
	if v.Members[0].Int != 13 || string(v.Members[1].Str) != "Hi" {
		t.Errorf("got n=%d s=%q", v.Members[0].Int, v.Members[1].Str)
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, _, err := per.Decode(pair, nil); err == nil {
		t.Error("an empty encoding decoded")
	}
	// the optional string is present but cut short
	if _, _, err := per.Decode(pair, []byte{0b1001_1010}); err == nil {
		t.Error("a truncated string decoded")
	}
}

// TestRandomEncodingsDecode checks the decoder against the test encoder, which follows the same rules,
// so it only catches slips in either; the real check is the comparison with libacars in the parent package.
func TestRandomEncodingsDecode(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 1000; i++ {
		buf := pertest.RandomEncoding(rng, pair)
		v, used, err := per.Decode(pair, buf)
		if err != nil || used != len(buf) {
			t.Fatalf("encoding %x: err=%v used=%d of %d", buf, err, used, len(buf))
		}
		if n := v.Members[0].Int; n < 10 || n > 25 {
			t.Fatalf("n out of range: %d", n)
		}
	}
}
