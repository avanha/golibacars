// Package mediaadv decodes ACARS Media Advisory messages (label SA): which datalinks the aircraft
// has established or lost, and which are available.
package mediaadv

import (
	"fmt"
	"strings"

	"github.com/avanha/golibacars/internal/ljson"
)

var links = map[byte]string{
	'V': "VHF ACARS",
	'S': "Default SATCOM",
	'H': "HF",
	'G': "Global Star Satcom",
	'C': "ICO Satcom",
	'2': "VDL2",
	'X': "Inmarsat Aero H/H+/I/L",
	'I': "Iridium Satcom",
}

// Msg is a media advisory. Err is set when the text is not one.
type Msg struct {
	Err            bool
	Version        int
	State          byte // 'E' established, 'L' lost
	CurrentLink    byte
	Hour, Min, Sec int
	AvailableLinks string
	FreeText       string
	hasText        bool
}

// Key implements proto.Node.
func (m *Msg) Key() string { return "media-adv" }

// Parse decodes a media advisory. It always returns a message; one that cannot be parsed has Err set.
func Parse(txt string) *Msg {
	if i := strings.IndexByte(txt, 0); i >= 0 {
		txt = txt[:i] // C reads the text up to a NUL
	}
	m := &Msg{Err: true}
	if len(txt) < 10 {
		return m
	}
	m.Version = int(txt[0]) - '0'
	if m.Version != 0 {
		return m
	}
	m.State = txt[1]
	if m.State != 'E' && m.State != 'L' {
		return m
	}
	m.CurrentLink = txt[2]
	if _, ok := links[m.CurrentLink]; !ok {
		return m
	}
	for i := 3; i < 9; i++ {
		if txt[i] < '0' || txt[i] > '9' {
			return m
		}
	}
	m.Hour = int(txt[3]-'0')*10 + int(txt[4]-'0')
	m.Min = int(txt[5]-'0')*10 + int(txt[6]-'0')
	m.Sec = int(txt[7]-'0')*10 + int(txt[8]-'0')
	if m.Hour > 23 || m.Min > 59 || m.Sec > 59 {
		return m
	}
	rest := txt[9:]
	i := 0
	for ; i < len(rest) && rest[i] != '/'; i++ {
		if _, ok := links[rest[i]]; !ok {
			return m
		}
	}
	m.AvailableLinks = rest[:i]
	if i < len(rest) && len(rest) > i+1 {
		m.FreeText, m.hasText = rest[i+1:], true
	}
	m.Err = false
	return m
}

// Text implements proto.Node.
func (m *Msg) Text(sb *strings.Builder, indent int) {
	pad := func(n int) string { return strings.Repeat(" ", n) }
	if m.Err {
		sb.WriteString(pad(indent) + "-- Unparseable Media Advisory message\n")
		return
	}
	fmt.Fprintf(sb, "%sMedia Advisory, version %d:\n", pad(indent), m.Version)
	indent++
	state := "lost"
	if m.State == 'E' {
		state = "established"
	}
	fmt.Fprintf(sb, "%sLink %s %s at %02d:%02d:%02d UTC\n", pad(indent), links[m.CurrentLink], state, m.Hour, m.Min, m.Sec)
	sb.WriteString(pad(indent) + "Available links: ")
	for i := 0; i < len(m.AvailableLinks); i++ {
		sb.WriteString(links[m.AvailableLinks[i]])
		if i == len(m.AvailableLinks)-1 {
			sb.WriteString("\n")
		} else {
			sb.WriteString(", ")
		}
	}
	if m.hasText && m.FreeText != "" {
		fmt.Fprintf(sb, "%sText: %s\n", pad(indent), m.FreeText)
	}
}

// JSON implements proto.Node.
func (m *Msg) JSON(w *ljson.W) {
	w.Bool("err", m.Err)
	if m.Err {
		return
	}
	w.Int64("version", int64(m.Version))
	w.ObjectStart("current_link")
	w.Bytes("code", []byte{m.CurrentLink})
	w.Str("descr", links[m.CurrentLink])
	w.Bool("established", m.State == 'E')
	w.ObjectStart("time")
	w.Int64("hour", int64(m.Hour))
	w.Int64("min", int64(m.Min))
	w.Int64("sec", int64(m.Sec))
	w.ObjectEnd()
	w.ObjectEnd()
	w.ArrayStart("links_avail")
	for i := 0; i < len(m.AvailableLinks); i++ {
		w.ObjectStart("")
		w.Bytes("code", []byte{m.AvailableLinks[i]})
		w.Str("descr", links[m.AvailableLinks[i]])
		w.ObjectEnd()
	}
	w.ArrayEnd()
	if m.hasText && m.FreeText != "" {
		w.Str("text", m.FreeText)
	}
}
