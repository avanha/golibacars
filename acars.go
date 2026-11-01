// Package acars decodes the application layers carried in ACARS message text (FANS-1/A CPDLC,
// ADS-C, MIAM, media advisories) and renders them exactly as libacars does.
//
// It is a Go port of libacars by Tomasz Lemiech. The output of the C library is the reference:
// the tests compare the two on real messages.
package acars

import (
	"github.com/avanha/golibacars/adsc"
	"github.com/avanha/golibacars/arinc"
	"github.com/avanha/golibacars/fans"
	"github.com/avanha/golibacars/internal/proto"
	"github.com/avanha/golibacars/mediaadv"
	"github.com/avanha/golibacars/miam"
	"github.com/avanha/golibacars/ohma"
)

// Dir is the direction of an ACARS message.
type Dir int

const (
	Unknown  Dir = 0
	Uplink   Dir = 1 // ground to air
	Downlink Dir = 2 // air to ground
)

// Result is a decoded message.
type Result struct {
	tree proto.Tree
}

// JSON returns libacars' JSON for the message.
func (r *Result) JSON() string { return r.tree.JSON() }

// Text returns libacars' text rendering of the message.
func (r *Result) Text() string { return r.tree.Text() }

// Decode decodes ACARS application text given the label of the message and its direction
// (la_acars_decode_apps). It returns nil if nothing in the text is recognised.
func Decode(label, text string, dir Dir) *Result {
	if len(label) < 2 {
		return nil
	}
	var tree proto.Tree
	switch label[0] {
	case 'A', 'B':
		if label[1] == '6' || label[1] == 'A' {
			tree = arinc.Parse(text, fans.Dir(dir), adsc.Parse)
		}
	case 'H':
		if label[1] == '1' {
			if tree = arinc.Parse(text, fans.Dir(dir), adsc.Parse); tree == nil {
				if tree = miam.Parse(text); tree == nil {
					tree = ohma.Parse(text)
				}
			}
		}
	case 'M':
		if label[1] == 'A' {
			tree = miam.Parse(text)
		}
	case 'S':
		if label[1] == 'A' {
			tree = proto.Tree{mediaadv.Parse(text)}
		}
	}
	if tree == nil {
		return nil
	}
	return &Result{tree}
}
