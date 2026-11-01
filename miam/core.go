// Package miam decodes MIAM (Media Independent Aircraft Messaging) frames and their MIAM CORE PDUs.
//
// Only single frames are decoded. Reassembly of file transfers, which libacars does when it is given
// a reassembly context, is not done: what Decode returns is what libacars returns without one.
// The optional XML reformatting of payloads (a libacars option that is off by default) is not supported.
package miam

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/avanha/golibacars/internal/crc"
	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/internal/proto"
)

// Error flags, as in miam-core.h. The low half are header errors, the high half body errors.
const (
	errHdrPDUTypeUnknown    = 1 << 1
	errHdrPDUVersionUnknown = 1 << 2
	errHdrTruncated         = 1 << 3
	errHdrAppTypeUnknown    = 1 << 4
	errBodyTruncated        = 1 << 16
	errBodyInflateFailed    = 1 << 17
	errBodyComprUnsupported = 1 << 18
	errBodyCRCFailed        = 1 << 19
	errHdr                  = 0x0000ffff
	errBody                 = 0xffff0000
)

const (
	pduData = iota
	pduAck
	pduAlo
	pduAlr
	pduUnknown
)

// node is a protocol layer made of closures.
type node struct {
	key  string
	text func(sb *strings.Builder, indent int)
	json func(w *ljson.W)
}

func (n *node) Key() string { return n.key }

func (n *node) Text(sb *strings.Builder, indent int) {
	if n.text != nil {
		n.text(sb, indent)
	}
}

func (n *node) JSON(w *ljson.W) {
	if n.json != nil {
		n.json(w)
	}
}

func printf(sb *strings.Builder, indent int, format string, a ...any) {
	sb.WriteString(strings.Repeat(" ", indent))
	fmt.Fprintf(sb, format, a...)
}

// cstr reads a fixed array the way C reads a string: up to the first NUL.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// base85 decodes the Adobe-style Base85 text libacars uses, including its habit of reading 'z' as four zero octets.
// The characters are taken as signed, as on the platforms libacars is usually built for.
func base85(s string) []byte {
	weights := [5]uint32{85 * 85 * 85 * 85, 85 * 85 * 85, 85 * 85, 85, 1}
	var out []byte
	i := 0
	at := func(i int) byte { // the C code reads the terminator here
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	for len(s)-i >= 5 || at(i) == 'z' {
		var v uint32
		if at(i) == 'z' {
			i++
		} else {
			for k := 0; k < 5; k, i = k+1, i+1 {
				v += uint32(int32(int8(s[i]))-0x21) * weights[k]
			}
		}
		out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
	return out
}

// Inflate decompresses raw deflate data (la_inflate). It returns what was produced even when the data is bad.
// Output is capped as libacars caps it: its buffer grows in steps of four times the input up to 1 MiB.
func Inflate(buf []byte) (out []byte, ok bool) {
	const maxLen = 1 << 20
	chunk := 4 * len(buf)
	limit := chunk * (maxLen / chunk)
	if limit < chunk {
		limit = chunk
	}
	out, ok = inflateRaw(buf, limit)
	if len(out) > limit {
		return out[:limit], false
	}
	return out, ok
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

// Hexdump is la_hexdump.
func Hexdump(data []byte) string {
	if len(data) == 0 {
		return "<none>"
	}
	const hex = "0123456789abcdef"
	var sb strings.Builder
	for i := 0; i < len(data); i += 16 {
		for j := i; j < i+16; j++ {
			if j < len(data) {
				sb.WriteByte(hex[data[j]>>4])
				sb.WriteByte(hex[data[j]&0xf])
			} else {
				sb.WriteString("  ")
			}
			sb.WriteByte(' ')
			if j == i+7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(" |")
		for j := i; j < i+16; j++ {
			if j < len(data) {
				if data[j] < 32 || data[j] > 126 {
					sb.WriteByte('.')
				} else {
					sb.WriteByte(data[j])
				}
			} else {
				sb.WriteByte(' ')
			}
			if j == i+7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}

// Multiline is la_isprintf_multiline_text: each line of txt indented, a trailing newline not producing an empty line.
func Multiline(sb *strings.Builder, indent int, txt string) {
	for {
		line := txt
		rest, more := "", false
		if i := strings.IndexByte(txt, '\n'); i >= 0 {
			line, rest, more = txt[:i], txt[i+1:], true
		}
		printf(sb, indent, "%s\n", line)
		if !more || rest == "" {
			return
		}
		txt = rest
	}
}

var errMessages = map[int]string{
	0:                       "No error",
	errHdrPDUTypeUnknown:    "Unknown PDU type",
	errHdrPDUVersionUnknown: "Unsupported MIAM version",
	errHdrTruncated:         "Header truncated",
	errHdrAppTypeUnknown:    "Unknown application type",
	errBodyTruncated:        "Message truncated",
	errBodyInflateFailed:    "Decompression failed",
	errBodyComprUnsupported: "Unsupported compression algorithm",
	errBodyCRCFailed:        "CRC check failed",
}

func errorsText(sb *strings.Builder, err uint32, indent int) {
	for i := uint(0); i < 32; i++ {
		if bit := err & (1 << i); bit != 0 {
			if m, ok := errMessages[int(bit)]; ok {
				printf(sb, indent, "-- %s\n", m)
			} else {
				printf(sb, indent, "-- Unknown error (%d)\n", err)
			}
		}
	}
}

func errorsJSON(w *ljson.W, err uint32) { w.Int64("err", int64(err)) }

func bitmaskText(sb *strings.Builder, mask uint8, names []string, indent int) {
	for i := 0; i < 8; i++ {
		if mask&(1<<uint(i)) != 0 {
			if i < len(names) {
				printf(sb, indent, "%s\n", names[i])
			} else {
				printf(sb, indent, "unknown (%d)\n", 1<<uint(i))
			}
		}
	}
}

func bitmaskJSON(w *ljson.W, mask uint8, names []string) {
	for i := 0; i < 8; i++ {
		if mask&(1<<uint(i)) != 0 {
			if i < len(names) {
				w.Str("", names[i])
			} else {
				w.Int64("", 1<<uint(i))
			}
		}
	}
}

var (
	aloCompression = []string{"deflate"}
	aloNetworks    = []string{"ACARS", "IP Middleware", "TCP/IP", "Satcom Data 3", "UDP"}
)

// parseCore decodes a MIAM CORE PDU from the text after the frame identifier (la_miam_core_pdu_parse).
// It returns nil if the text is not one.
func parseCore(txt string) proto.Tree {
	if i := strings.IndexByte(txt, 0); i >= 0 {
		txt = txt[:i]
	}
	if len(txt) < 3 {
		return nil
	}
	bpad, hpad := txt[0], txt[1]
	txt = txt[2:]
	if !(bpad >= '0' && bpad <= '3') && bpad != '-' && bpad != '.' {
		return nil
	}
	if !(hpad >= '0' && hpad <= '3') {
		return nil
	}
	hp := int(hpad - '0')
	d := strings.IndexByte(txt, '|')
	if d < 0 || d == 0 {
		return nil
	}
	header := base85(txt[:d])
	if len(header) < hp {
		return nil
	}
	var body []byte
	if d+1 < len(txt) {
		switch {
		case bpad >= '0' && bpad <= '3':
			body = base85(txt[d+1:])
			if bp := int(bpad - '0'); len(body) >= bp {
				body = body[:len(body)-bp]
			}
		case bpad == '-':
			body = []byte(txt[d+1:])
		}
	}
	// The header buffer is zero-filled beyond what was decoded, so a short one reads as zeros.
	b0 := byte(0)
	if len(header) > 0 {
		b0 = header[0]
	}
	header = header[:len(header)-hp]
	version, ptype := b0&0xf, (b0>>4)&0xf

	core := &corePDU{version: version, pduType: pduUnknown}
	cn := &node{key: "miam_core", text: core.text, json: core.json}
	if version != 1 && version != 2 {
		core.err |= errHdrPDUVersionUnknown
		return proto.Tree{cn}
	}
	var next proto.Node
	switch {
	case ptype == pduData && version == 1:
		next = parseV1Data(header, body)
	case ptype == pduAck && version == 1:
		next = parseV1Ack(header)
	case ptype == pduData && version == 2:
		next = parseV2Data(header, body)
	case ptype == pduAck && version == 2:
		next = parseV2Ack(header)
	case ptype == pduAlo:
		next = parseAloAlr(header, pduAlo)
	case ptype == pduAlr:
		next = parseAloAlr(header, pduAlr)
	default:
		core.err |= errHdrPDUTypeUnknown
		return proto.Tree{cn}
	}
	core.pduType = int(ptype)
	return proto.Tree{cn, next}
}

type corePDU struct {
	err     uint32
	version uint8
	pduType int
}

func (p *corePDU) text(sb *strings.Builder, indent int) {
	if p.err&errHdr != 0 {
		errorsText(sb, p.err&errHdr, indent)
		return
	}
	names := [...]string{"Data", "Ack", "Aloha", "Aloha Reply", "unknown PDU"}
	printf(sb, indent, "MIAM CORE %s, version %d:\n", names[p.pduType], p.version)
}

func (p *corePDU) json(w *ljson.W) {
	if p.err&errHdr != 0 {
		errorsJSON(w, p.err&errHdr)
		return
	}
	w.Int64("version", int64(p.version))
	w.Int64("pdu_type", int64(p.pduType))
}

type aloAlr struct {
	err         uint32
	pduLen      uint32
	aircraftID  [8]byte
	compression uint8
	networks    uint8
}

func parseAloAlr(h []byte, typ int) proto.Node {
	p := &aloAlr{}
	key, kind := "aloha", "supported"
	if typ == pduAlr {
		key, kind = "aloha_reply", "selected"
	}
	n := &node{key: key}
	n.text = func(sb *strings.Builder, indent int) {
		if p.err&errHdr != 0 {
			errorsText(sb, p.err&errHdr, indent)
			return
		}
		printf(sb, indent, "PDU Length: %d\n", p.pduLen)
		printf(sb, indent, "Aircraft ID: %s\n", cstr(p.aircraftID[:]))
		printf(sb, indent, "Compressions %s:\n", kind)
		bitmaskText(sb, p.compression, aloCompression, indent+1)
		printf(sb, indent, "Networks supported:\n")
		bitmaskText(sb, p.networks, aloNetworks, indent+1)
	}
	n.json = func(w *ljson.W) {
		if p.err&errHdr != 0 {
			errorsJSON(w, p.err&errHdr)
			return
		}
		w.Int64("pdu_len", int64(p.pduLen))
		w.Str("aircraft_id", cstr(p.aircraftID[:]))
		if typ == pduAlo {
			w.ArrayStart("comp_supported")
		} else {
			w.ArrayStart("comp_selected")
		}
		bitmaskJSON(w, p.compression, aloCompression)
		w.ArrayEnd()
		w.ArrayStart("networks_supported")
		bitmaskJSON(w, p.networks, aloNetworks)
		w.ArrayEnd()
	}
	if len(h) < 13 {
		p.err |= errHdrTruncated
		return n
	}
	p.pduLen = uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	copy(p.aircraftID[:7], h[4:11])
	p.compression, p.networks = h[11], h[12]
	return n
}

var xferNames = []string{"ack", "nack", "time_expiry", "peer_abort", "local_abort", "miam_version_not_supported"}

// dataPDU is a MIAM CORE data PDU of version 1 or 2.
type dataPDU struct {
	v2          bool
	err         uint32
	pduLen      uint32
	aircraftID  [8]byte
	msgNum      uint8
	ackOption   uint8
	compression uint8
	encoding    uint8
	appType     uint8
	appID       [9]byte
	crc         uint32
	data        []byte
	hasData     bool
}

func parseData(h, body []byte, v2 bool) proto.Node {
	p := &dataPDU{v2: v2}
	n := &node{key: "data", text: p.text, json: p.json}
	crcLen, minHdr := 4, 20
	if v2 {
		crcLen, minHdr = 2, 7
	}
	if len(h) < minHdr {
		p.err |= errHdrTruncated
		return n
	}
	if !v2 {
		p.pduLen = uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
		if p.pduLen > uint32(len(h)+len(body)) {
			p.err |= errBodyTruncated
		}
		copy(p.aircraftID[:7], h[4:11])
		h = h[11:]
	} else {
		h = h[1:]
	}
	p.msgNum = (h[0] >> 1) & 0x7f
	p.ackOption = h[0] & 1
	h = h[1:]
	p.compression = ((h[0] << 2) | ((h[1] >> 6) & 0x3)) & 0x7
	p.encoding = (h[1] >> 4) & 0x3
	p.appType = h[1] & 0xf
	h = h[2:]
	var idLen int
	switch {
	case p.appType == 0:
		idLen = 2
	case p.appType == 1:
		idLen = 4
	case p.appType == 2 || p.appType == 3:
		idLen = 6
	case v2 && p.appType&0x8 != 0 && p.appType != 0xd:
		idLen = int(p.appType&0x7) + 1
	default:
		p.err |= errHdrAppTypeUnknown
		return n
	}
	if len(h) < idLen+crcLen {
		p.err |= errHdrTruncated
		return n
	}
	copy(p.appID[:idLen], h[:idLen])
	p.appID[6] = 0
	h = h[idLen:]
	if v2 {
		p.crc = uint32(h[0])<<8 | uint32(h[1])
	} else {
		p.crc = uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	}
	if len(body) > 0 {
		switch p.compression {
		case 1:
			var ok bool
			p.data, ok = Inflate(body)
			p.hasData = true
			if !ok {
				p.err |= errBodyInflateFailed
			}
		case 0:
			p.data, p.hasData = append([]byte(nil), body...), true
		default:
			p.err |= errBodyComprUnsupported
		}
		var bad bool
		if v2 {
			bad = uint32(crc.CRC16ARINC(p.data, 0xFFFF)) != p.crc
		} else {
			bad = ^crc.CRC32ARINC665(p.data, 0xFFFFFFFF) != p.crc
		}
		if bad {
			p.err |= errBodyCRCFailed
		}
	}
	return n
}

func parseV1Data(h, body []byte) proto.Node { return parseData(h, body, false) }
func parseV2Data(h, body []byte) proto.Node { return parseData(h, body, true) }

func (p *dataPDU) text(sb *strings.Builder, indent int) {
	if p.err&errHdr != 0 {
		errorsText(sb, p.err&errHdr, indent)
		return
	}
	if !p.v2 {
		printf(sb, indent, "PDU Length: %d\n", p.pduLen)
		printf(sb, indent, "Aircraft ID: %s\n", cstr(p.aircraftID[:]))
	}
	printf(sb, indent, "Msg num: %d\n", p.msgNum)
	if p.ackOption == 1 {
		printf(sb, indent, "ACK: required\n")
	} else {
		printf(sb, indent, "ACK: not required\n")
	}
	switch p.compression {
	case 0:
		printf(sb, indent, "Compression: none\n")
	case 1:
		printf(sb, indent, "Compression: deflate\n")
	default:
		printf(sb, indent, "Compression: unknown (%d)\n", p.compression)
	}
	switch p.encoding {
	case 0:
		printf(sb, indent, "Encoding: ISO #5\n")
	case 1:
		printf(sb, indent, "Encoding: binary\n")
	default:
		printf(sb, indent, "Encoding: unknown (%d)\n", p.encoding)
	}
	id := p.appID
	switch {
	case p.appType <= 2:
		printf(sb, indent, "ACARS:\n")
		indent++
		printf(sb, indent, "Label: %c%c", id[0], id[1])
		if p.appType == 1 || p.appType == 2 {
			fmt.Fprintf(sb, " Sublabel: %c%c", id[2], id[3])
		}
		if p.appType == 2 {
			fmt.Fprintf(sb, " MFI: %c%c", id[4], id[5])
		}
		sb.WriteString("\n")
	case p.v2 && (p.appType >= 4 && p.appType <= 7 || p.appType == 0xd):
	default:
		printf(sb, indent, "Non-ACARS payload:\n")
		indent++
		printf(sb, indent, "Application ID: %s\n", cstr(id[:]))
	}
	if p.hasData {
		if isPrintable(p.data) {
			printf(sb, indent, "Message:\n")
			Multiline(sb, indent+1, string(p.data))
		} else {
			printf(sb, indent, "Message:\n")
			Multiline(sb, indent+1, Hexdump(p.data))
		}
	}
	if p.err&errBody != 0 {
		errorsText(sb, p.err&errBody, indent+1)
	}
}

func (p *dataPDU) json(w *ljson.W) {
	if p.err&errHdr != 0 {
		errorsJSON(w, p.err&errHdr)
		return
	}
	if !p.v2 {
		w.Int64("pdu_len", int64(p.pduLen))
		w.Str("aircraft_id", cstr(p.aircraftID[:]))
	}
	w.Int64("msg_num", int64(p.msgNum))
	w.Bool("ack_required", p.ackOption == 1)
	w.Int64("compression", int64(p.compression))
	w.Int64("encoding", int64(p.encoding))
	w.Int64("app_type", int64(p.appType))
	id := p.appID
	switch {
	case p.appType <= 2:
		w.ObjectStart("acars")
		w.Str("label", cstr([]byte{id[0], id[1], 0}))
		if p.appType == 1 || p.appType == 2 {
			w.Str("sublabel", cstr([]byte{id[2], id[3], 0}))
		}
		if p.appType == 2 {
			w.Str("mfi", cstr([]byte{id[4], id[5], 0}))
		}
	case p.v2 && (p.appType >= 4 && p.appType <= 7 || p.appType == 0xd):
	default:
		if p.v2 {
			w.ObjectStart("non_acars")
		} else if p.appType == 3 {
			w.ObjectStart("non_acars")
		} else {
			w.ObjectStart("unknown_payload_type")
		}
		if p.appType == 3 || p.v2 {
			w.Str("app_id", cstr(id[:]))
		}
	}
	w.ObjectStart("message")
	if p.hasData {
		if isPrintable(p.data) {
			w.Str("text", string(p.data))
		} else {
			w.OctetString("octet_string", p.data)
		}
	}
	if p.err&errBody != 0 {
		errorsJSON(w, p.err&errBody)
	}
	w.ObjectEnd()
	w.ObjectEnd()
}

type ack struct {
	v2         bool
	err        uint32
	pduLen     uint32
	aircraftID [8]byte
	ackNum     uint8
	result     uint8
}

func parseAck(h []byte, v2 bool) proto.Node {
	p := &ack{v2: v2}
	n := &node{key: "ack"}
	n.text = func(sb *strings.Builder, indent int) {
		if p.err&errHdr != 0 {
			errorsText(sb, p.err&errHdr, indent)
			return
		}
		if !v2 {
			printf(sb, indent, "PDU Length: %d\n", p.pduLen)
			printf(sb, indent, "Aircraft ID: %s\n", cstr(p.aircraftID[:]))
		}
		printf(sb, indent, "Msg ACK num: %d\n", p.ackNum)
		limit := 5 // version 1 knows five results
		if v2 {
			limit = 6
		}
		if int(p.result) < limit {
			printf(sb, indent, "Transfer result: %s\n", xferNames[p.result])
		} else {
			printf(sb, indent, "Transfer result: unknown (%d)\n", p.result)
		}
	}
	n.json = func(w *ljson.W) {
		if p.err&errHdr != 0 {
			errorsJSON(w, p.err&errHdr)
			return
		}
		if !v2 {
			w.Int64("pdu_len", int64(p.pduLen))
			w.Str("aircraft_id", cstr(p.aircraftID[:]))
		}
		w.Int64("msg_ack_num", int64(p.ackNum))
		w.Int64("ack_xfer_result", int64(p.result))
	}
	if !v2 {
		if len(h) < 20 {
			p.err |= errHdrTruncated
			return n
		}
		p.pduLen = uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
		copy(p.aircraftID[:7], h[4:11])
		p.ackNum = (h[11] >> 1) & 0x7f
		p.result = (h[12] >> 4) & 0xf
		return n
	}
	if len(h) < 8 {
		p.err |= errHdrTruncated
		return n
	}
	p.ackNum = (h[1] >> 1) & 0x7f
	p.result = ((h[1] << 3) | (h[2] >> 5)) & 0xf
	return n
}

func parseV1Ack(h []byte) proto.Node { return parseAck(h, false) }
func parseV2Ack(h []byte) proto.Node { return parseAck(h, true) }
