package ohma

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"strings"
	"testing"
)

func build(t *testing.T, js string) string {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	w.Write([]byte(js))
	w.Close()
	return "/BOMCAYA.OHMA" + base64.StdEncoding.EncodeToString(append([]byte{0x78, 0x9c}, buf.Bytes()...))
}

func TestParse(t *testing.T) {
	tree := Parse(build(t, `{"version":"1.0","convo_id":"C1","message":"hello","msg_seq":2,"msg_total":3}`))
	if tree == nil {
		t.Fatal("not recognised")
	}
	want := `{"ohma":{"err":0,"version":"1.0","msg_id":"C1","msg_seq":2,"msg_total":3,"reasm_status":"unknown","text":"hello"}}`
	if got := tree.JSON(); got != want {
		t.Errorf("got %s", got)
	}
	if txt := tree.Text(); !strings.Contains(txt, "Msg seq: 2") || !strings.Contains(txt, "Message:\n  hello\n") {
		t.Errorf("text %q", txt)
	}
}

func TestParseRefusals(t *testing.T) {
	for _, js := range []string{`{"version":1,"message":"x"}`, `[]`, `{"message":"x"}`, `{"version":"1","message":"x","msg_seq":1,"convo_id":"a"}`} {
		tree := Parse(build(t, js))
		if tree == nil || !strings.Contains(tree.JSON(), `"err":5`) && !strings.Contains(tree.JSON(), `"err":4`) {
			t.Errorf("%s: %v", js, tree)
		}
	}
	for _, txt := range []string{"", "OHM", "/BOMCAYA.XXXXabcd", "OHMA"} {
		if Parse(txt) != nil {
			t.Errorf("%q recognised", txt)
		}
	}
}

func TestJSONStrictness(t *testing.T) {
	bad := []string{`{"a":01}`, `{"a":"\u0000"}`, `{"a":"\ud800"}`, "{\"a\":\"\xff\"}", `{"a":1} x`, `"x"`, `{"a":1.}`, `{'a':1}`, `{"a":1e999}`, "{\"a\":\"\t\"}"}
	for _, s := range bad {
		if _, ok := parseJSON([]byte(s)); ok {
			t.Errorf("accepted %q", s)
		}
	}
	good := []string{`{}`, ` [ ] `, `{"a":-0,"a":[1,2.5e3,null,true,false,{"b":"é😀"}]}`}
	for _, s := range good {
		if _, ok := parseJSON([]byte(s)); !ok {
			t.Errorf("refused %q", s)
		}
	}
	if _, ok := parseJSON([]byte(strings.Repeat("[", 2048) + strings.Repeat("]", 2048))); !ok {
		t.Error("refused depth 2048")
	}
	if _, ok := parseJSON([]byte(strings.Repeat("[", 2049) + strings.Repeat("]", 2049))); ok {
		t.Error("accepted depth 2049")
	}
}
