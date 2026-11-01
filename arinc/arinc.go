// Package arinc decodes the ARINC 622 wrapper of ATS datalink messages: the ground address,
// the application identifier (AT1, ADS, ...), the aircraft registration and a CRC, followed by
// the binary application message in hexadecimal.
package arinc

import (
	"strings"

	"github.com/avanha/golibacars/fans"
	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/internal/proto"
)

// IMI is an application message identifier.
type IMI int

const (
	Unknown IMI = iota
	AT1
	CR1
	CC1
	DR1
	ADS
	DIS
)

var imiStrings = []struct {
	s   string
	imi IMI
}{{".AT1", AT1}, {".CR1", CR1}, {".CC1", CC1}, {".DR1", DR1}, {".ADS", ADS}, {".DIS", DIS}}

var props = map[IMI]struct{ desc, key string }{
	Unknown: {"Unknown message type", "unknown"},
	AT1:     {"FANS-1/A CPDLC Message", "fans1a_cpdlc_msg"},
	CR1:     {"FANS-1/A CPDLC Connect Request", "fans1a_cpdlc_connect_request"},
	CC1:     {"FANS-1/A CPDLC Connect Confirm", "fans1a_cpdlc_connect_confirm"},
	DR1:     {"FANS-1/A CPDLC Disconnect Request", "fans1a_cpdlc_disconnect_request"},
	ADS:     {"ADS-C message", "adsc_msg"},
	DIS:     {"ADS-C disconnect request", "adsc_disconnect_request"},
}

// Msg is the ARINC 622 layer.
type Msg struct {
	IMI     IMI
	GSAddr  string
	AirReg  string
	CRCOK   bool
	Payload []byte // the application message without its CRC
}

// Key implements proto.Node.
func (m *Msg) Key() string { return "arinc622" }

// Text implements proto.Node.
func (m *Msg) Text(sb *strings.Builder, indent int) {
	sb.WriteString(strings.Repeat(" ", indent) + props[m.IMI].desc + ":\n")
	if !m.CRCOK {
		sb.WriteString(strings.Repeat(" ", indent+1) + "-- CRC check failed\n")
	}
}

// JSON implements proto.Node.
func (m *Msg) JSON(w *ljson.W) {
	w.Str("msg_type", props[m.IMI].key)
	if m.IMI == Unknown {
		return
	}
	w.Bool("crc_ok", m.CRCOK)
	w.Str("gs_addr", m.GSAddr)
	w.Str("air_addr", m.AirReg)
}

func isNumericOrUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// guess finds the identifier and ground address (guess_arinc_msg_type). It returns the text from
// the identifier on, without its dot.
func guess(txt string) (imi IMI, gs, payload string, ok bool) {
	txt = strings.TrimPrefix(txt, "/")
	at := -1
	for _, c := range imiStrings {
		if i := strings.Index(txt, c.s); i >= 0 {
			imi, at = c.imi, i
			break
		}
	}
	if at < 0 {
		return 0, "", "", false
	}
	if (at == 7 || at == 4) && isNumericOrUpper(txt[:at]) {
		return imi, txt[:at], txt[at+1:], true
	}
	return 0, "", "", false
}

// slurpHex reads hexadecimal into bytes as la_slurp_hexstring does: an odd last digit is
// ignored, and an invalid character ends the data.
func slurpHex(s string) []byte {
	if len(s)&1 == 1 {
		s = s[:len(s)-1]
	}
	out := make([]byte, 0, len(s)/2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'A' && c <= 'F':
			v = 10 + c - 'A'
		case c >= 'a' && c <= 'f':
			v = 10 + c - 'a'
		default:
			return out[:i/2]
		}
		if i%2 == 0 {
			out = append(out, v<<4)
		} else {
			out[i/2] |= v
		}
	}
	return out
}

// Parse decodes the ARINC 622 layer of txt and what it carries. It returns nil if txt is not an ARINC 622 message.
func Parse(txt string, dir fans.Dir, adsc func(buf []byte, dir fans.Dir, imi IMI) proto.Node) proto.Tree {
	imi, gs, payload, ok := guess(txt)
	if !ok {
		return nil
	}
	m := &Msg{IMI: imi, GSAddr: gs}
	var next proto.Node
	// All the identifiers handled here carry a binary message.
	if len(payload) < 3+7+2*2 {
		return nil
	}
	m.AirReg = payload[3:10]
	buf := slurpHex(payload[10:])
	m.CRCOK = crcOK(payload[:10], buf)
	if len(buf) < 2 {
		// libacars subtracts the CRC length from an unsigned count here and reads past the buffer; what it
		// reports for the AT1 family is an undecodable message.
		if imi != ADS && imi != DIS {
			next = &fans.Message{Err: true}
		}
	} else {
		m.Payload = buf[:len(buf)-2]
		switch imi {
		case CR1, CC1, DR1, AT1:
			next = fans.Parse(m.Payload, fans.Dir(dir))
		case ADS, DIS:
			if adsc != nil {
				next = adsc(m.Payload, dir, imi)
			}
		}
	}
	if next == nil {
		return proto.Tree{m}
	}
	return proto.Tree{m, next}
}

// crc16 is CRC-16/CCITT-FALSE (poly 0x1021, MSB first), as la_crc16_arinc computes it.
func crc16(data []byte, crc uint16) uint16 {
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// crcOK checks the CRC over the identifier, registration and the binary message with its CRC.
func crcOK(text string, bin []byte) bool {
	buf := append([]byte(text), bin...)
	return crc16(buf, 0xFFFF) == 0x1D0F
}
