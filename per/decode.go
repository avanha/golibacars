package per

// Decode decodes one value of type t from the start of data, as uper_decode_complete does.
// It returns the value and the number of whole octets the encoding used.
func Decode(t *Type, data []byte) (*Value, int, error) {
	r := &reader{buf: data, nbits: 8 * len(data)}
	v, err := decode(r, t, nil)
	if err != nil {
		return nil, 0, err
	}
	used := (r.off + 7) / 8
	if used == 0 {
		// A value that takes no bits still has to be one zero octet.
		if len(data) == 0 {
			return nil, 0, ErrStarved
		}
		if data[0] != 0 {
			return nil, 0, ErrFailed
		}
		used = 1
	}
	return v, used, nil
}

// pick chooses the constraints for a type: the member's own, else the type's, else none.
func pick(t *Type, c *Constraints) *Constraints {
	if c != nil {
		return c
	}
	return t.Per
}

func decode(r *reader, t *Type, c *Constraints) (*Value, error) {
	v := &Value{Type: t}
	switch t.Kind {
	case Sequence:
		return v, decodeSequence(r, t, v)
	case Choice:
		return v, decodeChoice(r, t, c, v)
	case SequenceOf:
		return v, decodeSequenceOf(r, t, c, v)
	case Integer:
		return v, decodeInteger(r, t, c, v)
	case Enumerated:
		return v, decodeEnumerated(r, t, c, v)
	case Boolean:
		b, err := r.bits(1)
		v.Bool = b == 1
		return v, err
	case Null:
		return v, nil
	case IA5String, NumericString:
		return v, decodeString(r, t, c, v)
	}
	return nil, ErrFailed
}

func decodeSequence(r *reader, t *Type, v *Value) error {
	present := make([]bool, t.RootOptional)
	for i := range present {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		present[i] = b == 1
	}
	v.Members = make([]*Value, len(t.Members))
	opt := 0
	for i, m := range t.Members {
		if m.Optional {
			if opt >= len(present) {
				return ErrFailed
			}
			p := present[opt]
			opt++
			if !p {
				continue
			}
		}
		mv, err := decode(r, m.Type, m.Per)
		if err != nil {
			return err
		}
		v.Members[i] = mv
	}
	return nil
}

func decodeChoice(r *reader, t *Type, c *Constraints, v *Value) error {
	var ct *Constraint
	if p := pick(t, c); p != nil {
		ct = &p.Value
	}
	if ct != nil && ct.Flags&Extensible != 0 {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		if b == 1 {
			ct = nil
		}
	}
	// A CHOICE here is never extensible, so without a range there is no way to read the index.
	if ct == nil || ct.RangeBits < 0 {
		return ErrFailed
	}
	idx, err := r.bits(ct.RangeBits)
	if err != nil {
		return err
	}
	if int64(idx) > ct.Upper || int(idx) >= len(t.Members) {
		return ErrFailed
	}
	v.Choice = int(idx)
	m := t.Members[idx]
	mv, err := decode(r, m.Type, m.Per)
	if err != nil {
		return err
	}
	v.Members = []*Value{mv}
	return nil
}

func decodeSequenceOf(r *reader, t *Type, c *Constraints, v *Value) error {
	var ct *Constraint
	if p := pick(t, c); p != nil {
		ct = &p.Size
	}
	if ct != nil && ct.Flags&Extensible != 0 {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		if b == 1 {
			ct = nil
		}
	}
	n := -1
	if ct != nil && ct.EffBits >= 0 {
		k, err := r.bits(ct.EffBits)
		if err != nil {
			return err
		}
		n = int(k) + int(ct.Lower)
	}
	elm := t.Members[0]
	for {
		repeat := false
		if n < 0 {
			eb := -1
			if ct != nil {
				eb = ct.EffBits
			}
			var err error
			if n, repeat, err = r.length(eb); err != nil {
				return err
			}
		}
		for i := 0; i < n; i++ {
			ev, err := decode(r, elm.Type, elm.Per)
			if err != nil {
				return err
			}
			v.Items = append(v.Items, ev)
		}
		n = -1
		if !repeat {
			return nil
		}
	}
}

func decodeInteger(r *reader, t *Type, c *Constraints, v *Value) error {
	var ct *Constraint
	if p := pick(t, c); p != nil {
		ct = &p.Value
	}
	if ct != nil && ct.Flags&Extensible != 0 {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		if b == 1 {
			ct = nil
		}
	}
	if ct != nil && ct.Flags != Unconstrained && ct.RangeBits >= 0 {
		n, err := r.wholeNumber(ct.RangeBits)
		if err != nil {
			return err
		}
		v.Int = int64(n) + ct.Lower
		return nil
	}
	// Unconstrained or semi-constrained: length-prefixed two's complement octets.
	var buf []byte
	for {
		n, repeat, err := r.length(-1)
		if err != nil {
			return err
		}
		b, err := r.readBytes(8 * n)
		if err != nil {
			return err
		}
		buf = append(buf, b...)
		if !repeat {
			break
		}
	}
	if len(buf) == 0 || len(buf) > 8 {
		return ErrFailed
	}
	x := int64(int8(buf[0]))
	for _, b := range buf[1:] {
		x = x<<8 | int64(b)
	}
	if ct != nil && ct.Flags&SemiConstrained != 0 {
		x += ct.Lower
	}
	v.Int = x
	return nil
}

func decodeEnumerated(r *reader, t *Type, c *Constraints, v *Value) error {
	p := pick(t, c)
	if p == nil {
		return ErrFailed
	}
	ct := &p.Value
	if ct.Flags&Extensible != 0 {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		if b == 1 {
			return ErrFailed // no extension values are defined
		}
	}
	if ct.RangeBits < 0 {
		return ErrFailed
	}
	idx, err := r.bits(ct.RangeBits)
	if err != nil {
		return err
	}
	if int(idx) >= len(t.Enum) {
		return ErrFailed
	}
	v.Int = t.Enum[idx].Value
	return nil
}

// Default constraints of the character types, from asn1c's IA5String.c, NumericString.c and OCTET_STRING.c.
var (
	ia5Constraints = &Constraints{
		Value: Constraint{Flags: Constrained, RangeBits: 7, EffBits: 7, Lower: 0, Upper: 0x7f},
		Size:  Constraint{Flags: SemiConstrained, RangeBits: -1, EffBits: -1},
	}
	numericConstraints = &Constraints{
		Value: Constraint{Flags: Constrained, RangeBits: 4, EffBits: 4, Lower: 0x20, Upper: 0x39},
		Size:  Constraint{Flags: SemiConstrained, RangeBits: -1, EffBits: -1},
		Code2Value: func(code uint32) int {
			switch {
			case code == 0:
				return 0x20
			case code <= 10:
				return int(code) + 0x30 - 1
			}
			return -1
		},
	}
)

// DefaultConstraints returns the constraints a character type has when its definition gives none.
func DefaultConstraints(k Kind) *Constraints {
	switch k {
	case IA5String:
		return ia5Constraints
	case NumericString:
		return numericConstraints
	}
	return nil
}

func decodeString(r *reader, t *Type, c *Constraints, v *Value) error {
	pc := pick(t, c)
	if pc == nil {
		pc = DefaultConstraints(t.Kind)
	}
	cval, csiz := pc.Value, pc.Size
	unitBits := 8
	if cval.Flags&Constrained != 0 {
		unitBits = cval.RangeBits
	}
	if csiz.Flags&Extensible != 0 {
		b, err := r.bits(1)
		if err != nil {
			return err
		}
		if b == 1 {
			return ErrFailed // the string types here have no extensible sizes
		}
	}
	if csiz.EffBits == 0 {
		s, err := r.characters(int(csiz.Upper), unitBits, cval.Lower, cval.Upper, pc)
		v.Str = s
		return err
	}
	for {
		n, repeat, err := r.length(csiz.EffBits)
		if err != nil {
			return err
		}
		n += int(csiz.Lower)
		s, err := r.characters(n, unitBits, cval.Lower, cval.Upper, pc)
		if err != nil {
			return err
		}
		v.Str = append(v.Str, s...)
		if !repeat {
			return nil
		}
	}
}

// characters reads n characters of unitBits each, as OCTET_STRING_per_get_characters does.
func (r *reader) characters(n, unitBits int, lb, ub int64, pc *Constraints) ([]byte, error) {
	out := make([]byte, 0, n)
	if ub <= int64(2)<<uint(unitBits-1) {
		lb = 0 // no translation
	} else if pc.Code2Value != nil {
		for i := 0; i < n; i++ {
			code, err := r.bits(unitBits)
			if err != nil {
				return nil, err
			}
			val := pc.Code2Value(code)
			if val < 0 {
				return nil, ErrFailed
			}
			out = append(out, byte(val))
		}
		return out, nil
	}
	if lb == 0 && unitBits == 8 {
		return r.readBytes(8 * n)
	}
	for i := 0; i < n; i++ {
		code, err := r.bits(unitBits)
		if err != nil {
			return nil, err
		}
		ch := int64(code) + lb
		if ch > ub {
			return nil, ErrFailed
		}
		out = append(out, byte(ch))
	}
	return out, nil
}
