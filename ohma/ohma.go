// Package ohma decodes OHMA messages: Base64 text holding a deflated JSON envelope.
//
// Only a single message is decoded; libacars' reassembly of multipart messages needs state kept
// between calls and is not done, as in libacars when it is not given a reassembly context.
package ohma

import (
	"fmt"
	"strings"

	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/internal/proto"
	"github.com/avanha/golibacars/miam"
)

// Decoding errors, in the order libacars numbers them.
const (
	errSuccess = iota
	errTooShort
	errUnknownCompression
	errDecompressionFailed
	errJSONDecodeFailed
	errJSONBadStructure
)

var errText = [...]string{
	"Success", "Message too short", "Unknown compression algorithm",
	"Decompression failed", "Failed to decode message as JSON", "Unexpected JSON structure",
}

// Msg is an OHMA message.
type Msg struct {
	err      int
	version  string
	hasVer   bool
	convoID  string
	hasConvo bool
	symKey   []byte
	iv       []byte
	sig      []byte
	payload  []byte
	hasLoad  bool
	msgSeq   int32
	msgTotal int32
	skipped  bool // reassembly status "skipped" rather than "unknown"
}

// Key implements proto.Node.
func (m *Msg) Key() string { return "ohma" }

func (m *Msg) reasm() string {
	if m.skipped {
		return "skipped"
	}
	return "unknown"
}

func isPrintable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if !(c >= 7 && c <= 13) && !(c >= 32 && c <= 126) {
			return false
		}
	}
	return true
}

// base64Decode is la_base64_decode: the input is cut to whole groups of four, and decoding stops
// once the output length (less the padding) is reached. It returns nil if the input is empty or not Base64.
func base64Decode(in string) []byte {
	if len(in) == 0 {
		return nil
	}
	n := len(in) &^ 3
	if n == 0 {
		// libacars still looks at the two characters before the input here, which are never padding;
		// the result is empty.
		return []byte{}
	}
	dlen := n * 3 / 4
	if in[n-1] == '=' {
		dlen--
	}
	if in[n-2] == '=' {
		dlen--
	}
	idx := func(c byte) int {
		return strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", c)
	}
	out := make([]byte, 0, dlen)
	for i := 0; i < n; {
		i1, i2 := idx(in[i]), idx(in[i+1])
		if i1 < 0 || i2 < 0 {
			return nil
		}
		i += 2
		out = append(out, byte(i1<<2|i2>>4))
		if len(out) >= dlen {
			break
		}
		i3 := idx(in[i])
		if i3 < 0 {
			return nil
		}
		i++
		out = append(out, byte(i2<<4|i3>>2))
		if len(out) >= dlen {
			break
		}
		i4 := idx(in[i])
		if i4 < 0 {
			return nil
		}
		i++
		out = append(out, byte(i3<<6|i4))
	}
	return out
}

// Parse decodes an OHMA message from the text of an ACARS message. It returns nil if the text is not one.
func Parse(txt string) proto.Tree {
	if i := strings.IndexByte(txt, 0); i >= 0 {
		txt = txt[:i]
	}
	ptr := 0
	for {
		rest := txt[ptr:]
		if len(rest) >= 13 && rest[0] == '/' && rest[8] == '.' {
			ptr += 9
		} else if len(rest) >= 8 && rest[0] == '/' && rest[3] == '.' {
			ptr += 4
		}
		if !strings.HasPrefix(txt[ptr:], "OHMA") && !strings.HasPrefix(txt[ptr:], "RYKO") {
			return nil
		}
		ptr += 4
		// A first fragment that was sent twice shows up as a second copy of what came before; skip to it.
		dup := strings.Index(txt[ptr:], txt[:ptr])
		if dup < 0 {
			break
		}
		ptr += dup
	}
	body := strings.TrimRight(txt[ptr:], "\r\n")
	raw := base64Decode(body)
	if raw == nil {
		return nil
	}
	m := &Msg{}
	tree := proto.Tree{m}
	if len(raw) < 3 {
		m.err = errTooShort
		return tree
	}
	if raw[0]&0xf != 8 {
		m.err = errUnknownCompression
		return tree
	}
	inflated, ok := miam.Inflate(raw[2:])
	if !ok {
		m.err = errDecompressionFailed
		return tree
	}
	m.payload, m.hasLoad = inflated, true // what is kept if the JSON turns out to be unusable
	root, ok := parseJSON(inflated)
	if !ok {
		m.err = errJSONDecodeFailed
		return tree
	}
	if root.kind != kObject {
		m.err = errJSONBadStructure
		return tree
	}
	str := func(key string, required bool) (s string, present, ok bool) {
		v := root.obj[key]
		if v == nil {
			return "", false, !required
		}
		if v.kind != kString {
			return "", false, false
		}
		return v.s, true, true
	}
	integer := func(key string) (n int32, ok bool) {
		v := root.obj[key]
		if v == nil {
			return 0, true
		}
		if v.kind != kInt {
			return 0, false
		}
		return int32(v.i), true
	}
	version, hasVer, ok1 := str("version", true)
	convo, hasConvo, ok2 := str("convo_id", false)
	message, _, ok3 := str("message", true)
	seq, ok4 := integer("msg_seq")
	total, ok5 := integer("msg_total")
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		m.err = errJSONBadStructure
		return tree
	}
	m.version, m.hasVer = version, hasVer
	m.convoID, m.hasConvo = convo, hasConvo
	// The three encryption parameters are read together; if any is not a string, none are used.
	symKey, _, a := str("sym_key", false)
	iv, _, b := str("iv", false)
	sig, _, c := str("signature", false)
	if a && b && c {
		m.symKey, m.iv, m.sig = base64Decode(symKey), base64Decode(iv), base64Decode(sig)
	}
	if seq > 0 {
		m.msgSeq = seq
		if !hasConvo || (seq == 1 && total == 0) {
			m.err = errJSONBadStructure
			return tree
		}
		m.msgTotal = total
	} else {
		m.skipped = true
	}
	m.payload, m.hasLoad = []byte(message), true
	return tree
}

// Text implements proto.Node.
func (m *Msg) Text(sb *strings.Builder, indent int) {
	pad := func(n int) string { return strings.Repeat(" ", n) }
	fmt.Fprintf(sb, "%sOHMA message:\n", pad(indent))
	indent++
	if m.err != errSuccess {
		fmt.Fprintf(sb, "%s-- %s\n", pad(indent), errText[m.err])
	} else {
		if m.hasVer {
			fmt.Fprintf(sb, "%sVersion: %s\n", pad(indent), m.version)
		}
		if m.hasConvo {
			fmt.Fprintf(sb, "%sMsg ID: %s\n", pad(indent), m.convoID)
		}
		if m.msgSeq > 0 {
			fmt.Fprintf(sb, "%sMsg seq: %d\n", pad(indent), m.msgSeq)
		}
		if m.msgTotal > 0 {
			fmt.Fprintf(sb, "%sMsg total: %d\n", pad(indent), m.msgTotal)
		}
		fmt.Fprintf(sb, "%sReassembly: %s\n", pad(indent), m.reasm())
		for _, f := range []struct {
			name string
			b    []byte
		}{{"Sym key", m.symKey}, {"IV", m.iv}, {"Signature", m.sig}} {
			if f.b != nil {
				fmt.Fprintf(sb, "%s%s:\n", pad(indent), f.name)
				miam.Multiline(sb, indent+1, miam.Hexdump(f.b))
			}
		}
	}
	if m.hasLoad {
		if isPrintable(m.payload) {
			fmt.Fprintf(sb, "%sMessage:\n", pad(indent))
			miam.Multiline(sb, indent+1, string(m.payload))
		} else {
			fmt.Fprintf(sb, "%sData (%d bytes):\n", pad(indent), len(m.payload))
			miam.Multiline(sb, indent+1, miam.Hexdump(m.payload))
		}
	}
}

// JSON implements proto.Node.
func (m *Msg) JSON(w *ljson.W) {
	w.Int64("err", int64(m.err))
	if m.err != errSuccess {
		return
	}
	if m.hasVer {
		w.Str("version", m.version)
	}
	if m.hasConvo {
		w.Str("msg_id", m.convoID)
	}
	if m.msgSeq > 0 {
		w.Int64("msg_seq", int64(m.msgSeq))
	}
	if m.msgTotal > 0 {
		w.Int64("msg_total", int64(m.msgTotal))
	}
	w.Str("reasm_status", m.reasm())
	if m.symKey != nil {
		w.OctetString("sym_key", m.symKey)
	}
	if m.iv != nil {
		w.OctetString("iv", m.iv)
	}
	if m.sig != nil {
		w.OctetString("signature", m.sig)
	}
	if m.hasLoad {
		if isPrintable(m.payload) {
			w.Bytes("text", m.payload)
		} else {
			w.OctetString("octet_string", m.payload)
		}
	}
}
