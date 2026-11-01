// Package proto holds the decoded layers of a message and renders them as libacars does.
package proto

import (
	"strings"

	"github.com/avanha/golibacars/internal/ljson"
)

// Node is one decoded protocol layer. A message is a chain of them, outermost first.
type Node interface {
	// Key is the name of the layer's JSON object.
	Key() string
	// Text appends the layer's text rendering.
	Text(sb *strings.Builder, indent int)
	// JSON appends the members of the layer's JSON object.
	JSON(w *ljson.W)
}

// Tree is a chain of layers.
type Tree []Node

// JSON renders the chain, each layer nested in the one before it.
func (t Tree) JSON() string {
	var w ljson.W
	w.Start()
	for _, n := range t {
		w.ObjectStart(n.Key())
		n.JSON(&w)
	}
	for range t {
		w.ObjectEnd()
	}
	w.End()
	return w.String()
}

// Text renders the chain, each layer indented one step further than the one before it.
func (t Tree) Text() string {
	var sb strings.Builder
	for i, n := range t {
		n.Text(&sb, i)
	}
	return sb.String()
}
