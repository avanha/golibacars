package miam

// A raw-deflate decompressor that behaves like zlib's inflate() on bad input: it produces every
// symbol that is completely present, and keeps what it has produced when the data is cut short
// or corrupt. Go's compress/flate gives up one symbol earlier, which libacars' output shows.
// The structure follows puff.c from the zlib distribution.

type huffman struct {
	count  [16]int16
	symbol []int16
}

type inflater struct {
	in     []byte
	pos    int // next byte
	bitBuf uint32
	bitCnt uint
	out    []byte
	failed bool
}

// bits returns the next n bits, or -1 when the input has ended.
func (s *inflater) bits(n uint) int {
	for s.bitCnt < n {
		if s.pos >= len(s.in) {
			return -1
		}
		s.bitBuf |= uint32(s.in[s.pos]) << s.bitCnt
		s.pos++
		s.bitCnt += 8
	}
	v := int(s.bitBuf & (1<<n - 1))
	s.bitBuf >>= n
	s.bitCnt -= n
	return v
}

// construct builds a decoding table from code lengths. It returns the table, and whether the code is
// complete; it fails for an over-subscribed code.
func construct(lengths []int16) (h *huffman, left int, ok bool) {
	h = &huffman{symbol: make([]int16, len(lengths))}
	for _, l := range lengths {
		h.count[l]++
	}
	if int(h.count[0]) == len(lengths) {
		return h, 0, true
	}
	left = 1
	for l := 1; l <= 15; l++ {
		left <<= 1
		left -= int(h.count[l])
		if left < 0 {
			return h, left, false
		}
	}
	var offs [16]int16
	for l := 1; l < 15; l++ {
		offs[l+1] = offs[l] + h.count[l]
	}
	for sym, l := range lengths {
		if l != 0 {
			h.symbol[offs[l]] = int16(sym)
			offs[l]++
		}
	}
	return h, left, true
}

// decode reads one symbol, or returns -1 if the input ends or the code is invalid.
func (s *inflater) decode(h *huffman) int {
	code, first, index := 0, 0, 0
	for l := 1; l <= 15; l++ {
		b := s.bits(1)
		if b < 0 {
			return -1
		}
		code |= b
		count := int(h.count[l])
		if code-count < first {
			return int(h.symbol[index+(code-first)])
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	s.failed = true
	return -1
}

var (
	lenBase  = [29]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
	lenExtra = [29]uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	distBase = [30]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
	distExtr = [30]uint{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
)

// codes decodes the symbols of a Huffman-coded block. It returns false when it stops early.
func (s *inflater) codes(lencode, distcode *huffman) bool {
	for {
		sym := s.decode(lencode)
		if sym < 0 {
			return false
		}
		if sym < 256 {
			s.out = append(s.out, byte(sym))
			continue
		}
		if sym == 256 {
			return true
		}
		sym -= 257
		if sym >= 29 {
			s.failed = true
			return false
		}
		e := s.bits(lenExtra[sym])
		if e < 0 {
			return false
		}
		length := lenBase[sym] + e
		ds := s.decode(distcode)
		if ds < 0 {
			return false
		}
		if ds >= 30 {
			s.failed = true
			return false
		}
		de := s.bits(distExtr[ds])
		if de < 0 {
			return false
		}
		dist := distBase[ds] + de
		if dist > len(s.out) {
			s.failed = true
			return false
		}
		for i := 0; i < length; i++ {
			s.out = append(s.out, s.out[len(s.out)-dist])
		}
	}
}

var fixedLen, fixedDist *huffman

func init() {
	var l [288]int16
	for i := range l {
		switch {
		case i < 144:
			l[i] = 8
		case i < 256:
			l[i] = 9
		case i < 280:
			l[i] = 7
		default:
			l[i] = 8
		}
	}
	fixedLen, _, _ = construct(l[:])
	var d [30]int16
	for i := range d {
		d[i] = 5
	}
	fixedDist, _, _ = construct(d[:])
}

var order = [19]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

func (s *inflater) dynamic() bool {
	nlen, ndist, ncode := s.bits(5), s.bits(5), s.bits(4)
	if nlen < 0 || ndist < 0 || ncode < 0 {
		return false
	}
	nlen += 257
	ndist++
	ncode += 4
	if nlen > 286 || ndist > 30 {
		s.failed = true
		return false
	}
	var lengths [320]int16
	for i := 0; i < ncode; i++ {
		v := s.bits(3)
		if v < 0 {
			return false
		}
		lengths[order[i]] = int16(v)
	}
	lc, left, ok := construct(lengths[:19])
	if !ok || left != 0 {
		s.failed = true
		return false
	}
	var ls [320]int16
	for idx := 0; idx < nlen+ndist; {
		sym := s.decode(lc)
		if sym < 0 {
			return false
		}
		if sym < 16 {
			ls[idx] = int16(sym)
			idx++
			continue
		}
		var prev int16
		var rep int
		switch sym {
		case 16:
			if idx == 0 {
				s.failed = true
				return false
			}
			prev = ls[idx-1]
			v := s.bits(2)
			if v < 0 {
				return false
			}
			rep = 3 + v
		case 17:
			v := s.bits(3)
			if v < 0 {
				return false
			}
			rep = 3 + v
		default:
			v := s.bits(7)
			if v < 0 {
				return false
			}
			rep = 11 + v
		}
		if idx+rep > nlen+ndist {
			s.failed = true
			return false
		}
		for ; rep > 0; rep-- {
			ls[idx] = prev
			idx++
		}
	}
	if ls[256] == 0 {
		s.failed = true
		return false
	}
	lencode, left, ok := construct(ls[:nlen])
	if !ok || (left > 0 && nlen != int(lencode.count[0])+int(lencode.count[1])) {
		s.failed = true
		return false
	}
	distcode, left, ok := construct(ls[nlen : nlen+ndist])
	if !ok || (left > 0 && ndist != int(distcode.count[0])+int(distcode.count[1])) {
		s.failed = true
		return false
	}
	return s.codes(lencode, distcode)
}

func (s *inflater) stored() bool {
	s.bitBuf, s.bitCnt = 0, 0
	if s.pos+4 > len(s.in) {
		return false
	}
	n := int(s.in[s.pos]) | int(s.in[s.pos+1])<<8
	nn := int(s.in[s.pos+2]) | int(s.in[s.pos+3])<<8
	s.pos += 4
	if n != ^nn&0xffff {
		s.failed = true
		return false
	}
	avail := len(s.in) - s.pos
	if n > avail {
		s.out = append(s.out, s.in[s.pos:]...)
		s.pos = len(s.in)
		return false
	}
	s.out = append(s.out, s.in[s.pos:s.pos+n]...)
	s.pos += n
	return true
}

// inflateRaw decompresses a raw deflate stream. ok is true only if the final block was completed.
func inflateRaw(in []byte, limit int) (out []byte, ok bool) {
	s := &inflater{in: in}
	for {
		last := s.bits(1)
		typ := s.bits(2)
		if last < 0 || typ < 0 {
			break
		}
		var done bool
		switch typ {
		case 0:
			done = s.stored()
		case 1:
			done = s.codes(fixedLen, fixedDist)
		case 2:
			done = s.dynamic()
		default:
			s.failed = true
		}
		if !done || s.failed {
			break
		}
		if last == 1 {
			return s.out, true
		}
		if len(s.out) > limit {
			break
		}
	}
	return s.out, false
}
