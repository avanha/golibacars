// Package pertest generates random encodings of ASN.1 types, to test decoders against another implementation.
package pertest

import (
	"math/rand"

	"github.com/avanha/golibacars/per"
)

// bitWriter builds an unaligned PER encoding.
type bitWriter struct {
	buf  []byte
	nbit int
}

func (w *bitWriter) put(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.nbit%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.buf[len(w.buf)-1] |= 1 << (7 - uint(w.nbit%8))
		}
		w.nbit++
	}
}

func (w *bitWriter) length(n int) {
	if n < 128 {
		w.put(uint64(n), 8)
	} else {
		w.put(uint64(0x8000|n), 16)
	}
}

// RandomEncoding returns a valid encoding of a random value of type t, to test decoders.
// It follows the rules the decoder follows for the constraints asn1c generated, and supports only what FANS-1/A uses.
func RandomEncoding(rng *rand.Rand, t *per.Type) []byte {
	w := &bitWriter{}
	encode(rng, w, t, nil)
	return w.buf
}

func pickC(t *per.Type, c *per.Constraints) *per.Constraints {
	if c != nil {
		return c
	}
	return t.Per
}

func encode(rng *rand.Rand, w *bitWriter, t *per.Type, c *per.Constraints) {
	switch t.Kind {
	case per.Sequence:
		var present []bool
		for i := 0; i < t.RootOptional; i++ {
			p := rng.Intn(2) == 0
			present = append(present, p)
			if p {
				w.put(1, 1)
			} else {
				w.put(0, 1)
			}
		}
		opt := 0
		for _, m := range t.Members {
			if m.Optional {
				p := present[opt]
				opt++
				if !p {
					continue
				}
			}
			encode(rng, w, m.Type, m.Per)
		}
	case per.Choice:
		ct := pickC(t, c).Value
		idx := rng.Intn(int(ct.Upper) + 1)
		w.put(uint64(idx), ct.RangeBits)
		encode(rng, w, t.Members[idx].Type, t.Members[idx].Per)
	case per.SequenceOf:
		ct := pickC(t, c).Size
		var n int
		if ct.EffBits >= 0 {
			n = int(ct.Lower) + rng.Intn(int(ct.Upper-ct.Lower)+1)
			w.put(uint64(n-int(ct.Lower)), ct.EffBits)
		} else {
			n = rng.Intn(4)
			w.length(n)
		}
		for i := 0; i < n; i++ {
			encode(rng, w, t.Members[0].Type, t.Members[0].Per)
		}
	case per.Integer:
		ct := pickC(t, c).Value
		span := ct.Upper - ct.Lower + 1
		w.put(uint64(rng.Int63n(span)), ct.RangeBits)
	case per.Enumerated:
		ct := pickC(t, c).Value
		w.put(uint64(rng.Intn(len(t.Enum))), ct.RangeBits)
	case per.Boolean:
		w.put(uint64(rng.Intn(2)), 1)
	case per.Null:
	case per.IA5String, per.NumericString:
		pc := pickC(t, c)
		if pc == nil {
			pc = per.DefaultConstraints(t.Kind)
		}
		cs, cv := pc.Size, pc.Value
		var n int
		if cs.EffBits == 0 {
			n = int(cs.Upper)
		} else {
			n = int(cs.Lower)
			if cs.EffBits >= 0 {
				n += rng.Intn(int(cs.Upper-cs.Lower) + 1)
				w.put(uint64(n-int(cs.Lower)), cs.EffBits)
			} else {
				n += rng.Intn(10)
				w.length(n)
			}
		}
		for i := 0; i < n; i++ {
			if pc.Code2Value != nil {
				for {
					code := rng.Intn(1 << uint(cv.RangeBits))
					if pc.Code2Value(uint32(code)) >= 0 {
						w.put(uint64(code), cv.RangeBits)
						break
					}
				}
			} else {
				w.put(uint64(rng.Intn(1<<uint(cv.RangeBits))), cv.RangeBits)
			}
		}
	}
}
