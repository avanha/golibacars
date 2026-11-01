package acars

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"

	"github.com/avanha/golibacars/fans"
	"github.com/avanha/golibacars/internal/crc"
	"github.com/avanha/golibacars/per/pertest"
)

// Generators of synthetic messages, shared by the differential tests and the golden file writer.

// randomCPDLC returns the text of a valid random CPDLC message.
func randomCPDLC(rng *rand.Rand) (string, Dir) {
	dir := Dir(1 + rng.Intn(2))
	payload := pertest.RandomEncoding(rng, fans.MessageType(fans.Dir(dir)))
	full := append(payload, byte(rng.Intn(256)), byte(rng.Intn(256)))
	return cpdlcText(full), dir
}

var (
	adscDownLen = map[byte]int{3: 1, 6: 0, 7: 10, 9: 10, 10: 10, 12: 6, 13: 17, 14: 5, 15: 5, 16: 4, 17: 3, 18: 10, 19: 10, 20: 10, 22: 8, 23: 9}
	adscDown    = []byte{3, 6, 7, 9, 10, 12, 13, 14, 15, 16, 17, 18, 19, 20, 22, 23}
	adscReqLen  = map[byte]int{10: 1, 11: 1, 12: 1, 13: 1, 14: 1, 15: 1, 16: 1, 17: 1, 18: 1, 19: 4, 20: 0, 21: 2}
	adscReq     = []byte{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}
)

// randomADSC returns the text of a random ADS-C message whose tags have the right lengths.
func randomADSC(rng *rand.Rand) (string, Dir) {
	rb := func(k int) []byte { b := make([]byte, k); rng.Read(b); return b }
	var payload []byte
	dir := Dir(1 + rng.Intn(2))
	for k := 1 + rng.Intn(4); k > 0; k-- {
		if dir == Uplink {
			switch rng.Intn(5) {
			case 0:
				payload = append(payload, 1)
			case 1, 2:
				payload = append(payload, byte(2+4*rng.Intn(2)))
				payload = append(payload, rb(1)...)
			default:
				payload = append(payload, byte(7+rng.Intn(3)), byte(rng.Intn(256)))
				for j := rng.Intn(5); j > 0; j-- {
					tg := adscReq[rng.Intn(len(adscReq))]
					payload = append(payload, tg)
					payload = append(payload, rb(adscReqLen[tg])...)
				}
			}
			continue
		}
		switch rng.Intn(8) {
		case 0: // negative acknowledgement
			reason := byte(rng.Intn(15))
			payload = append(payload, 4, byte(rng.Intn(256)), reason)
			if reason == 1 || reason == 2 || reason == 7 {
				payload = append(payload, byte(rng.Intn(256)))
			}
		case 1: // noncompliance notification
			cnt := rng.Intn(4)
			payload = append(payload, 5, byte(rng.Intn(256)), byte(cnt))
			for g := 0; g < cnt; g++ {
				flags := byte(rng.Intn(256))
				payload = append(payload, byte(rng.Intn(24)), flags)
				if flags&0xc0 == 0 {
					payload = append(payload, rb(int(flags&0xf+1)/2)...)
				}
			}
		default:
			tg := adscDown[rng.Intn(len(adscDown))]
			payload = append(payload, tg)
			payload = append(payload, rb(adscDownLen[tg])...)
		}
	}
	full := append(payload, byte(rng.Intn(256)), byte(rng.Intn(256)))
	return "/BOMCAYA.ADS..VT-AN" + strings.ToUpper(hex.EncodeToString(full)), dir
}

// cpdlcText wraps a payload as an ARINC 622 message text.
func cpdlcText(payload []byte) string {
	return "/BOMCAYA.AT1..VT-AN" + strings.ToUpper(hex.EncodeToString(payload))
}

// ---- MIAM ----

// base85 encodes b as libacars' Base85, using 'z' for a group of four zero octets when zero is set.
func base85Encode(b []byte, useZ bool) string {
	var sb strings.Builder
	for i := 0; i+4 <= len(b); i += 4 {
		v := uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		if v == 0 && useZ {
			sb.WriteByte('z')
			continue
		}
		var c [5]byte
		for k := 4; k >= 0; k-- {
			c[k] = byte(v%85) + 0x21
			v /= 85
		}
		sb.Write(c[:])
	}
	return sb.String()
}

func pad4(b []byte) ([]byte, int) {
	k := (4 - len(b)%4) % 4
	return append(append([]byte(nil), b...), make([]byte, k)...), k
}

// randomMIAMBody returns a message body, deflated (and possibly damaged) or not.
func randomMIAMBody(rng *rand.Rand) (data, wire []byte, compression int) {
	words := []string{"A350", "REP053", "VHHH", "YMML", "CPA105", "H01", "TB000000", "ART3500008", "0354", "-12.5", "\r\n"}
	var sb strings.Builder
	for i := rng.Intn(60); i >= 0; i-- {
		sb.WriteString(words[rng.Intn(len(words))])
		sb.WriteByte(",/;"[rng.Intn(3)])
	}
	data = []byte(sb.String())
	if rng.Intn(6) == 0 { // binary
		data = make([]byte, rng.Intn(40))
		rng.Read(data)
	}
	if rng.Intn(4) == 0 {
		return data, data, 0
	}
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, []int{flate.NoCompression, flate.BestSpeed, flate.BestCompression, flate.HuffmanOnly}[rng.Intn(4)])
	w.Write(data)
	w.Close()
	wire = buf.Bytes()
	switch rng.Intn(6) {
	case 0: // truncated
		if len(wire) > 2 {
			wire = wire[:1+rng.Intn(len(wire)-1)]
		}
	case 1: // damaged
		if len(wire) > 0 {
			wire = append([]byte(nil), wire...)
			wire[rng.Intn(len(wire))] ^= 1 << uint(rng.Intn(8))
		}
	}
	return data, wire, 1
}

func randomMIAMCore(rng *rand.Rand) string {
	ver := 1 + rng.Intn(2)
	ptype := rng.Intn(4)
	if rng.Intn(20) == 0 {
		ver, ptype = rng.Intn(4), rng.Intn(8)
	}
	hdr := []byte{byte(ptype<<4 | ver)}
	rb := func(n int) []byte { b := make([]byte, n); rng.Read(b); return b }
	acid := []byte("..B-LRD")
	var body []byte
	bpad := byte('.')
	switch {
	case ptype == 0 && ver == 1, ptype == 0 && ver == 2:
		data, wire, comp := randomMIAMBody(rng)
		app := rng.Intn(4)
		if ver == 2 && rng.Intn(3) == 0 {
			app = []int{8, 9, 10, 11, 12, 14, 15, 5, 13, 3}[rng.Intn(10)]
		}
		idLen := map[int]int{0: 2, 1: 4, 2: 6, 3: 6}[app]
		if app >= 8 {
			idLen = app&7 + 1
		}
		if ver == 1 {
			n := len(wire) + 20
			if rng.Intn(5) == 0 {
				n += 50 // claims more than there is
			}
			hdr = append(hdr, byte(n>>16), byte(n>>8), byte(n))
			hdr = append(hdr, acid...)
		}
		hdr = append(hdr, byte(rng.Intn(128)<<1|rng.Intn(2)), 0, byte(comp&3)<<6|byte(rng.Intn(2))<<4|byte(app))
		id := []byte("H1DFAB")
		if idLen > len(id) {
			id = append(id, rb(idLen-len(id))...)
		}
		hdr = append(hdr, id[:idLen]...)
		crcOK := rng.Intn(4) != 0
		if ver == 1 {
			c := ^crc32ARINC(data)
			if !crcOK {
				c++
			}
			hdr = append(hdr, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
		} else {
			c := crc16ARINC(data)
			if !crcOK {
				c++
			}
			hdr = append(hdr, byte(c>>8), byte(c))
		}
		if comp == 0 && rng.Intn(2) == 0 && !bytes.ContainsAny(wire, "|") && !bytes.ContainsRune(wire, 0) {
			bpad, body = '-', wire
		} else {
			p, k := pad4(wire)
			bpad, body = byte('0'+k), []byte(base85Encode(p, rng.Intn(2) == 0))
		}
	case ptype == 1: // ack
		hdr = append(hdr, rb(3)...)
		hdr = append(hdr, acid...)
		hdr = append(hdr, rb(12)...)
	default: // aloha, aloha reply
		hdr = append(hdr, 0, 0, 16)
		hdr = append(hdr, acid...)
		hdr = append(hdr, byte(rng.Intn(4)), byte(rng.Intn(256)), 0, 0, 0)
	}
	switch rng.Intn(10) {
	case 0: // header too short
		hdr = hdr[:1+rng.Intn(len(hdr))]
	}
	p, hp := pad4(hdr)
	s := string(rune(bpad)) + string(rune('0'+hp)) + base85Encode(p, rng.Intn(2) == 0) + "|" + string(body)
	return s
}

// randomMIAM returns a MIAM frame of a random kind.
func randomMIAM(rng *rand.Rand) (label, text string, dir Dir) {
	label = []string{"MA", "H1"}[rng.Intn(2)]
	dir = Dir(1 + rng.Intn(2))
	d3 := func() string { return fmt.Sprintf("%03d", rng.Intn(1000)) }
	switch rng.Intn(9) {
	case 0, 1, 2:
		return label, "T" + randomMIAMCore(rng), dir
	case 3:
		return label, "F" + d3() + fmt.Sprintf("%06d", rng.Intn(1000000)) + fmt.Sprintf("%02d%02d%02d%02d%02d%02d", rng.Intn(100), rng.Intn(14), rng.Intn(33), rng.Intn(25), rng.Intn(61), rng.Intn(61)) + []string{"", "\r\n"}[rng.Intn(2)], dir
	case 4:
		return label, "K" + d3() + string("0123456789ABCDEFG"[rng.Intn(17)]) + d3() + d3(), dir
	case 5:
		return label, "S" + d3() + d3() + randomMIAMCore(rng), dir
	case 6:
		return label, "A" + d3() + string("0123456789"[rng.Intn(10)]), dir
	case 7:
		id := d3()
		if rng.Intn(3) == 0 {
			id = "FFF"
		}
		return label, "Y" + id, dir
	default:
		id := d3()
		if rng.Intn(3) == 0 {
			id = "FFF"
		}
		return label, "X" + id + d3() + d3(), dir
	}
}

func crc16ARINC(b []byte) uint16 { return crc.CRC16ARINC(b, 0xFFFF) }
func crc32ARINC(b []byte) uint32 { return crc.CRC32ARINC665(b, 0xFFFFFFFF) }

// ---- OHMA ----

// randomOHMAJSON returns the text of an OHMA envelope, often with something wrong in it.
func randomOHMAJSON(rng *rand.Rand) string {
	b64 := func() string {
		b := make([]byte, rng.Intn(40))
		rng.Read(b)
		return base64.StdEncoding.EncodeToString(b)
	}
	pick := func(opts ...string) string { return opts[rng.Intn(len(opts))] }
	good := rng.Intn(2) == 0 // half of the envelopes have the right types and all the required fields
	str := func(s string) string {
		if good {
			return `"` + s + `"`
		}
		return pick(`"`+s+`"`, `"`+s+`"`, `"`+s+`"`, "null", "7", "1.5", "true", `["x"]`, `{}`)
	}
	var f []string
	add := func(key, val string) {
		if good && (key == "msg_seq" || key == "msg_total") {
			val = pick("1", "2", "3", "0", "5")
		}
		if good && rng.Intn(3) == 0 && key != "version" && key != "message" {
			return
		}
		if good || rng.Intn(8) != 0 {
			f = append(f, `"`+key+`":`+val)
		}
	}
	add("version", str(pick("1.0", "2", "")))
	add("convo_id", str(pick("A1B2C3", "x y", "é€😀", `aé\n`, "")))
	add("message", str(pick("hello", "line1\\nline2", "", `{\"a\":1}`, "\\u0001ctl", "café")))
	add("msg_seq", pick("1", "2", "0", "-3", "3", "4294967297", "1.0", `"1"`, "null", "9223372036854775807"))
	add("msg_total", pick("1", "3", "0", "7", "-1", "2.5"))
	add("sym_key", str(b64()))
	add("iv", str(b64()))
	add("signature", str(b64()))
	if rng.Intn(6) == 0 {
		add("extra", pick(`[1,2,{"a":null}]`, `"x"`, "-0", "1e5", "123456789012345678901234567890"))
	}
	if rng.Intn(10) == 0 && len(f) > 0 {
		f = append(f, f[rng.Intn(len(f))]) // repeated key
	}
	rng.Shuffle(len(f), func(i, j int) { f[i], f[j] = f[j], f[i] })
	s := "{" + strings.Join(f, pick(",", " , ", ",\n")) + "}"
	m := rng.Intn(12)
	if good && rng.Intn(4) != 0 {
		m = 99
	}
	switch m {
	case 0:
		s = "[" + s + "]"
	case 1:
		s += pick(" ", "\n", "x", "{}", ",")
	case 2:
		s = pick(" ", "\t") + s
	case 3:
		s = strings.Replace(s, `"`, pick("'", `\"`, `“`), 1)
	case 4:
		b := []byte(s)
		if len(b) > 0 {
			b[rng.Intn(len(b))] = byte(rng.Intn(256))
		}
		s = string(b)
	case 5:
		s = strings.Replace(s, "1.0", pick("01.0", "1.", ".5", "-", "1e", "+1"), 1)
	case 6:
		s = strings.Repeat("[", 1+rng.Intn(3000)) + strings.Repeat("]", 1+rng.Intn(3000))
	case 7:
		s = strings.Replace(s, "hello", pick(`😀`, `\ud83d`, `\ude00`, `\u0000`, `\x41`, "\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80"), 1)
	}
	return s
}

// randomOHMA returns the text of an OHMA message (or something near it).
func randomOHMA(rng *rand.Rand) (string, Dir) {
	js := randomOHMAJSON(rng)
	var raw []byte
	switch rng.Intn(14) {
	case 0:
		raw = []byte{0x77, 0x9c} // wrong compression method
		raw = append(raw, js...)
	case 1:
		raw = []byte{0x78}
	case 2:
		raw = append([]byte{0x78, 0x9c}, js...) // not deflated
	default:
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, []int{flate.NoCompression, flate.BestSpeed, flate.BestCompression}[rng.Intn(3)])
		w.Write([]byte(js))
		w.Close()
		z := buf.Bytes()
		if rng.Intn(10) == 0 && len(z) > 2 {
			z = z[:1+rng.Intn(len(z)-1)]
		}
		raw = append([]byte{0x78, 0x9c}, z...)
	}
	enc := base64.StdEncoding.EncodeToString(raw)
	switch rng.Intn(10) {
	case 0:
		enc = strings.TrimRight(enc, "=")
	case 1:
		enc += "\r\n"
	case 2:
		enc = enc[:len(enc)-rng.Intn(3)]
	case 3:
		b := []byte(enc)
		b[rng.Intn(len(b))] = "!_ -"[rng.Intn(4)]
		enc = string(b)
	}
	marker := []string{"OHMA", "RYKO", "OHMA", "OHMX"}[rng.Intn(4)]
	var prefix string
	switch rng.Intn(5) {
	case 0:
		prefix = "/BOMCAYA."
	case 1:
		prefix = "/AB."
	case 2:
		prefix = ""
	case 3:
		prefix = "/BOMCAYA.AB"
	default:
		prefix = "/GSABCDE."
	}
	text := prefix + marker + enc
	if rng.Intn(8) == 0 {
		text += prefix + marker + enc // the first fragment sent twice
	}
	return text, Dir(1 + rng.Intn(2))
}
