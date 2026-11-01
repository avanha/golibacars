// Package per decodes ASN.1 values from unaligned PER (X.691), driven by type tables.
//
// It is a port of the decoding half of asn1c 0.9.28, the compiler that produced the tables
// libacars uses, and follows it on purpose, including where it differs from the standard.
// Only the parts that FANS-1/A needs are here: no extension markers, open types or
// unconstrained integers beyond what a plain length determinant gives.
package per

import "errors"

// ErrStarved means the encoding ended before the type was complete.
var ErrStarved = errors.New("per: out of data")

// ErrFailed means the data cannot be a value of the type.
var ErrFailed = errors.New("per: invalid encoding")

// reader is a bit cursor over a byte slice.
type reader struct {
	buf   []byte
	nbits int // total bits available
	off   int // bits consumed
}

// bits returns the next n bits (n <= 32) as an unsigned number.
func (r *reader) bits(n int) (uint32, error) {
	if n < 0 {
		return 0, ErrFailed
	}
	if n > r.nbits-r.off {
		return 0, ErrStarved
	}
	var v uint64
	for i := 0; i < n; i++ {
		p := r.off + i
		v = v<<1 | uint64(r.buf[p>>3]>>(7-uint(p&7))&1)
	}
	r.off += n
	return uint32(v), nil
}

// wholeNumber reads a constrained whole number of n bits, as uper_get_constrained_whole_number does.
func (r *reader) wholeNumber(n int) (uint64, error) {
	if n <= 31 {
		v, err := r.bits(n)
		return uint64(v), err
	}
	if n > 64 {
		return 0, ErrFailed
	}
	hi, err := r.bits(31)
	if err != nil {
		return 0, err
	}
	lo, err := r.wholeNumber(n - 31)
	if err != nil {
		return 0, err
	}
	return uint64(hi)<<uint(n-31) | lo, nil
}

// length reads a length determinant. With ebits >= 0 it is a constrained length of that many bits.
// repeat reports a fragment of 16K * m items, after which another length follows.
func (r *reader) length(ebits int) (n int, repeat bool, err error) {
	if ebits >= 0 {
		v, err := r.bits(ebits)
		return int(v), false, err
	}
	v, err := r.bits(8)
	if err != nil {
		return 0, false, err
	}
	if v&128 == 0 {
		return int(v & 0x7f), false, nil
	}
	if v&64 == 0 {
		w, err := r.bits(8)
		if err != nil {
			return 0, false, err
		}
		return int((v&63)<<8 | w), false, nil
	}
	m := v & 63
	if m < 1 || m > 4 {
		return 0, false, ErrFailed
	}
	return int(16384 * m), true, nil
}

// readBytes copies nbits bits into a byte slice, the way per_get_many_bits does with alright=0:
// whole bytes first, then the last few bits left-aligned.
func (r *reader) readBytes(nbits int) ([]byte, error) {
	if nbits > r.nbits-r.off {
		return nil, ErrStarved
	}
	out := make([]byte, (nbits+7)/8)
	for i := 0; i < nbits; i++ {
		p := r.off + i
		if r.buf[p>>3]>>(7-uint(p&7))&1 == 1 {
			out[i>>3] |= 1 << (7 - uint(i&7))
		}
	}
	r.off += nbits
	return out, nil
}
