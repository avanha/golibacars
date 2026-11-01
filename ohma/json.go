package ohma

// A JSON parser with the acceptance rules of jansson 2.x called with no flags, which is what libacars uses
// to read OHMA messages: only an object or array at the top, no trailing data, strict UTF-8, no \u0000,
// no leading zeros, integers that fit in 64 bits, and nesting up to 2048 deep.

import (
	"math"
	"strconv"
	"unicode/utf8"
)

const maxDepth = 2048

type kind uint8

const (
	kNull kind = iota
	kBool
	kInt
	kReal
	kString
	kArray
	kObject
)

type value struct {
	kind kind
	i    int64
	s    string
	obj  map[string]*value
}

type parser struct {
	b     []byte
	pos   int
	depth int
	bad   bool
}

// parseJSON returns the value, and false if jansson would refuse the text.
func parseJSON(b []byte) (*value, bool) {
	p := &parser{b: b}
	p.skipSpace()
	if p.pos >= len(b) || (b[p.pos] != '{' && b[p.pos] != '[') {
		return nil, false
	}
	v := p.value()
	if p.bad {
		return nil, false
	}
	p.skipSpace()
	if p.pos != len(b) {
		return nil, false
	}
	return v, true
}

func (p *parser) fail() *value { p.bad = true; return nil }

func (p *parser) skipSpace() {
	for p.pos < len(p.b) {
		switch p.b[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value() *value {
	p.skipSpace()
	if p.pos >= len(p.b) {
		return p.fail()
	}
	switch c := p.b[p.pos]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		s, ok := p.str()
		if !ok {
			return p.fail()
		}
		return &value{kind: kString, s: s}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.b) && p.b[p.pos] >= 'a' && p.b[p.pos] <= 'z' {
			p.pos++
		}
		switch string(p.b[start:p.pos]) {
		case "true":
			return &value{kind: kBool, i: 1}
		case "false":
			return &value{kind: kBool}
		case "null":
			return &value{kind: kNull}
		}
	}
	return p.fail()
}

func (p *parser) enter() bool {
	p.depth++
	return p.depth <= maxDepth
}

func (p *parser) array() *value {
	if !p.enter() {
		return p.fail()
	}
	p.pos++
	v := &value{kind: kArray}
	p.skipSpace()
	if p.pos < len(p.b) && p.b[p.pos] == ']' {
		p.pos++
		p.depth--
		return v
	}
	for {
		if p.value(); p.bad {
			return nil
		}
		p.skipSpace()
		if p.pos >= len(p.b) {
			return p.fail()
		}
		c := p.b[p.pos]
		p.pos++
		if c == ']' {
			p.depth--
			return v
		}
		if c != ',' {
			return p.fail()
		}
	}
}

func (p *parser) object() *value {
	if !p.enter() {
		return p.fail()
	}
	p.pos++
	v := &value{kind: kObject, obj: map[string]*value{}}
	p.skipSpace()
	if p.pos < len(p.b) && p.b[p.pos] == '}' {
		p.pos++
		p.depth--
		return v
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.b) || p.b[p.pos] != '"' {
			return p.fail()
		}
		key, ok := p.str()
		if !ok {
			return p.fail()
		}
		p.skipSpace()
		if p.pos >= len(p.b) || p.b[p.pos] != ':' {
			return p.fail()
		}
		p.pos++
		val := p.value()
		if p.bad {
			return nil
		}
		v.obj[key] = val // a repeated key replaces the earlier value
		p.skipSpace()
		if p.pos >= len(p.b) {
			return p.fail()
		}
		c := p.b[p.pos]
		p.pos++
		if c == '}' {
			p.depth--
			return v
		}
		if c != ',' {
			return p.fail()
		}
	}
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var v rune
	for _, c := range b[:4] {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			v |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}

// str reads a string starting at the opening quote.
func (p *parser) str() (string, bool) {
	p.pos++
	var out []byte
	for p.pos < len(p.b) {
		c := p.b[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out), true
		case c < 0x20:
			return "", false
		case c == '\\':
			p.pos++
			if p.pos >= len(p.b) {
				return "", false
			}
			e := p.b[p.pos]
			p.pos++
			switch e {
			case '"', '\\', '/':
				out = append(out, e)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				r, ok := hex4(p.b[p.pos:])
				if !ok {
					return "", false
				}
				p.pos += 4
				switch {
				case r == 0:
					return "", false // \u0000 needs JSON_ALLOW_NUL
				case r >= 0xD800 && r <= 0xDBFF:
					if p.pos+6 > len(p.b) || p.b[p.pos] != '\\' || p.b[p.pos+1] != 'u' {
						return "", false
					}
					lo, ok := hex4(p.b[p.pos+2:])
					if !ok || lo < 0xDC00 || lo > 0xDFFF {
						return "", false
					}
					p.pos += 6
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				case r >= 0xDC00 && r <= 0xDFFF:
					return "", false
				}
				out = utf8.AppendRune(out, r)
			default:
				return "", false
			}
		case c < 0x80:
			out = append(out, c)
			p.pos++
		default:
			r, n := utf8.DecodeRune(p.b[p.pos:])
			if r == utf8.RuneError && n <= 1 {
				return "", false
			}
			out = append(out, p.b[p.pos:p.pos+n]...)
			p.pos += n
		}
	}
	return "", false
}

func (p *parser) number() *value {
	start := p.pos
	if p.b[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.b) {
		return p.fail()
	}
	if p.b[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.b) && p.b[p.pos] >= '0' && p.b[p.pos] <= '9' {
			return p.fail()
		}
	} else if p.b[p.pos] >= '1' && p.b[p.pos] <= '9' {
		for p.pos < len(p.b) && p.b[p.pos] >= '0' && p.b[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return p.fail()
	}
	real := false
	if p.pos < len(p.b) && p.b[p.pos] == '.' {
		real = true
		p.pos++
		d := p.pos
		for p.pos < len(p.b) && p.b[p.pos] >= '0' && p.b[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == d {
			return p.fail()
		}
	}
	if p.pos < len(p.b) && (p.b[p.pos] == 'e' || p.b[p.pos] == 'E') {
		real = true
		p.pos++
		if p.pos < len(p.b) && (p.b[p.pos] == '+' || p.b[p.pos] == '-') {
			p.pos++
		}
		d := p.pos
		for p.pos < len(p.b) && p.b[p.pos] >= '0' && p.b[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == d {
			return p.fail()
		}
	}
	text := string(p.b[start:p.pos])
	if !real {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return p.fail()
		}
		return &value{kind: kInt, i: n}
	}
	f, err := strconv.ParseFloat(text, 64)
	if math.IsInf(f, 0) || (err != nil && f != 0) {
		return p.fail()
	}
	return &value{kind: kReal}
}
