// Package ljson writes JSON the way libacars' json.c does: compact, with every value followed
// by a comma that the closing bracket removes, and with raw bytes escaped one by one.
package ljson

import (
	"fmt"
	"strings"
)

// W is a JSON writer.
type W struct {
	b []byte
}

// String returns what has been written.
func (w *W) String() string { return string(w.b) }

func (w *W) trimComma() {
	if n := len(w.b); n > 0 && w.b[n-1] == ',' {
		w.b = w.b[:n-1]
	}
}

func (w *W) key(k string) {
	if k != "" {
		w.b = append(w.b, '"')
		w.b = append(w.b, k...)
		w.b = append(w.b, '"', ':')
	}
}

// Bool appends "key":true or "key":false.
func (w *W) Bool(key string, v bool) {
	w.key(key)
	if v {
		w.b = append(w.b, "true,"...)
	} else {
		w.b = append(w.b, "false,"...)
	}
}

// Double appends a number with six decimals, as C's %f does.
func (w *W) Double(key string, v float64) {
	w.key(key)
	w.b = append(w.b, fmt.Sprintf("%f,", v)...)
}

// Int64 appends an integer.
func (w *W) Int64(key string, v int64) {
	w.key(key)
	w.b = append(w.b, fmt.Sprintf("%d,", v)...)
}

// Bytes appends a string made of raw bytes.
func (w *W) Bytes(key string, v []byte) {
	w.key(key)
	w.b = append(w.b, '"')
	for _, c := range v {
		if c < ' ' || c > 0x7e || c == '"' || c == '\\' {
			w.b = append(w.b, '\\')
			switch c {
			case '\b':
				w.b = append(w.b, 'b')
			case '\t':
				w.b = append(w.b, 't')
			case '\n':
				w.b = append(w.b, 'n')
			case '\f':
				w.b = append(w.b, 'f')
			case '\r':
				w.b = append(w.b, 'r')
			case '"':
				w.b = append(w.b, '"')
			case '\\':
				w.b = append(w.b, '\\')
			default:
				w.b = append(w.b, fmt.Sprintf("u%04x", c)...)
			}
		} else {
			w.b = append(w.b, c)
		}
	}
	w.b = append(w.b, '"', ',')
}

// Str appends a string, which C would have read up to its first NUL.
func (w *W) Str(key, v string) {
	if i := strings.IndexByte(v, 0); i >= 0 {
		v = v[:i]
	}
	w.Bytes(key, []byte(v))
}

// ObjectStart opens an object.
func (w *W) ObjectStart(key string) { w.key(key); w.b = append(w.b, '{') }

// ObjectEnd closes an object.
func (w *W) ObjectEnd() { w.trimComma(); w.b = append(w.b, '}', ',') }

// ArrayStart opens an array.
func (w *W) ArrayStart(key string) { w.key(key); w.b = append(w.b, '[') }

// ArrayEnd closes an array.
func (w *W) ArrayEnd() { w.trimComma(); w.b = append(w.b, ']', ',') }

// OctetString appends bytes as an array of numbers.
func (w *W) OctetString(key string, v []byte) {
	w.ArrayStart(key)
	for _, c := range v {
		w.Int64("", int64(c))
	}
	w.ArrayEnd()
}

// Start begins the top-level object.
func (w *W) Start() { w.ObjectStart("") }

// End finishes the top-level object, leaving no trailing comma.
func (w *W) End() { w.ObjectEnd(); w.trimComma() }
