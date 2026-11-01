//go:build oracle

// Command dump runs every input of a corpus through libacars and writes the results as JSON lines,
// so the port can be tested against them without cgo.
//
//	go run -tags oracle ./internal/oracle/cmd/dump in.jsonl > golden.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/avanha/golibacars/internal/oracle"
)

type Case struct {
	Label string `json:"label"`
	Dir   int    `json:"dir"`
	Text  string `json:"text"`
	OK    bool   `json:"ok"`
	JSON  string `json:"json,omitempty"`
	Out   string `json:"out,omitempty"`
}

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for sc.Scan() {
		var c Case
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			panic(err)
		}
		c.JSON, c.Out, c.OK = oracle.Decode(c.Label, c.Text, oracle.Dir(c.Dir))
		b, _ := json.Marshal(c)
		fmt.Fprintln(w, string(b))
	}
}
