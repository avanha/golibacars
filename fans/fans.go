// Package fans decodes FANS-1/A CPDLC messages (the ATC uplink and downlink PDUs of ARINC 622)
// and formats them as libacars does.
//
// The types come from libacars' generated tables (types_gen.go, formats_gen.go), made by cmd/gen.
package fans

import (
	"fmt"
	"strings"

	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/per"
)

// Dir is the direction of a message.
type Dir int

const (
	Uplink   Dir = 1 // ground to air
	Downlink Dir = 2 // air to ground
)

// Message is a decoded CPDLC PDU.
type Message struct {
	Err   bool       // the payload could not be decoded
	Empty bool       // the payload was empty (not an error)
	Value *per.Value // the decoded PDU when Err and Empty are false
}

// MessageType returns the ASN.1 type of the PDU in a direction.
func MessageType(dir Dir) *per.Type {
	if dir == Uplink {
		return tFANSATCUplinkMessage
	}
	return tFANSATCDownlinkMessage
}

// Parse decodes a PDU. A payload that is empty is allowed; one that does not decode, or that leaves octets over, is an error.
func Parse(buf []byte, dir Dir) *Message {
	if len(buf) == 0 {
		return &Message{Empty: true}
	}
	v, used, err := per.Decode(MessageType(dir), buf)
	if err != nil || used < len(buf) {
		return &Message{Err: true}
	}
	return &Message{Value: v}
}

// Key implements proto.Node.
func (m *Message) Key() string { return "cpdlc" }

// params is the state of one formatting call (la_asn1_formatter_params).
type params struct {
	text   *strings.Builder
	json   *ljson.W
	v      *per.Value
	label  string
	hasLbl bool
	indent int
}

// isprintf appends an indented formatted line fragment (LA_ISPRINTF).
func (p *params) isprintf(indent int, format string, a ...any) {
	p.text.WriteString(strings.Repeat(" ", indent))
	fmt.Fprintf(p.text, format, a...)
}

// Text appends libacars' text rendering of the message at the given indent.
func (m *Message) Text(sb *strings.Builder, indent int) {
	p := &params{text: sb, indent: indent}
	if m.Err {
		p.isprintf(indent, "-- Unparseable FANS-1/A message\n")
		return
	}
	if m.Value == nil {
		p.isprintf(indent, "-- <empty PDU>\n")
		return
	}
	p.v = m.Value
	output(p, textTable, true)
}

// JSON appends libacars' JSON rendering of the message (the members of the "cpdlc" object).
func (m *Message) JSON(w *ljson.W) {
	w.Bool("err", m.Err)
	if m.Err || m.Value == nil {
		return
	}
	output(&params{json: w, v: m.Value}, jsonTable, false)
}
