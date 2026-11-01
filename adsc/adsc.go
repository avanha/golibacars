// Package adsc decodes ADS-C (automatic dependent surveillance, contract) messages, as libacars does.
package adsc

import (
	"fmt"
	"math"
	"strings"

	"github.com/avanha/golibacars/arinc"
	"github.com/avanha/golibacars/fans"
	"github.com/avanha/golibacars/internal/ljson"
	"github.com/avanha/golibacars/internal/proto"
)

// bits reads big-endian bit fields from a byte slice.
type bits struct {
	buf []byte
	pos int
}

func (b *bits) read(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | uint32(b.buf[b.pos>>3]>>(7-uint(b.pos&7))&1)
		b.pos++
	}
	return v
}

// signExtend interprets the low n bits of v as a two's complement number.
func signExtend(v uint32, n uint) int {
	return int(int32(v<<(32-n)) >> (32 - n))
}

func coordinate(c uint32) float64 {
	r := signExtend(c, 21)
	result := 180.0 - 90.0/math.Pow(2, 19)
	result *= float64(r)
	result /= float64(0xfffff)
	return result
}

func altitude(a uint32) int { return signExtend(a, 16) * 4 }

func timestamp(t uint32) float64 { return float64(t) * 0.125 }

func speed(s uint32) float64 { return float64(s) / 2.0 }

func vertSpeed(vs uint32) int { return signExtend(vs, 12) * 16 }

func distance(d uint32) float64 { return float64(d) / 8.0 }

func heading(h uint32) float64 {
	r := signExtend(h, 12)
	result := 180.0 - 90.0/math.Pow(2, 10)
	result *= float64(r)
	result /= float64(0x7ff)
	if result < 0.0 {
		result += 360.0
	}
	return result
}

func windDir(w uint32) float64 {
	r := signExtend(w, 9)
	result := 180.0 - 90.0/math.Pow(2, 7)
	result *= float64(r)
	result /= float64(0xff)
	if result < 0.0 {
		result += 360.0
	}
	return result
}

func temperature(t uint32) float64 {
	r := signExtend(t, 12)
	result := 512.0 - 256.0/math.Pow(2, 10)
	result *= float64(r)
	result /= float64(0x7ff)
	return result
}

// ctx is the state shared by the formatters: where the output goes and the current indent.
type ctx struct {
	sb     *strings.Builder
	w      *ljson.W
	indent int
}

func (c *ctx) printf(format string, a ...any) {
	c.sb.WriteString(strings.Repeat(" ", c.indent))
	fmt.Fprintf(c.sb, format, a...)
}

// typeDesc describes one kind of tag (la_adsc_type_descriptor_t). parse returns the number of
// octets used, or -1.
type typeDesc struct {
	label string
	key   string
	parse func(t *tag, buf []byte) int
	text  func(c *ctx, label string, data any)
	json  func(c *ctx, data any)
}

type tag struct {
	id   uint8
	typ  *typeDesc // nil if the tag could not be parsed
	data any
}

type basicReport struct {
	lat, lon                   float64
	alt                        int
	timestamp                  float64
	redundancy, accuracy, tcas uint8
}

type flightID struct{ id string }

type predictedRoute struct {
	latNext, lonNext         float64
	altNext, etaNext         int
	latNextNext, lonNextNext float64
	altNextNext              int
}

type earthAirRef struct {
	headingInvalid bool
	heading, speed float64
	vertSpeed      int
}

type intermediateProjection struct {
	distance     float64
	trackInvalid bool
	track        float64
	alt, eta     int
}

type fixedProjection struct {
	lat, lon float64
	alt, eta int
}

type meteo struct {
	windSpeed      float64
	windDirInvalid bool
	windDir, temp  float64
}

type nack struct {
	contractReqNum, reason, extData uint8
}

type noncompGroup struct {
	tag                           uint8
	unrecognized, wholeGroupUnavl bool
	paramCnt                      int
	params                        [15]uint8
}

type noncompNotify struct {
	contractReqNum uint8
	groups         []noncompGroup
}

type reportInterval struct{ scalingFactor, rate uint8 }

type acftIntentReq struct{ modulus, projTime uint8 }

type altRange struct{ ceiling, floor int }

type request struct {
	contractNum uint8
	tags        []*tag
}

// Msg is a decoded ADS-C message.
type Msg struct {
	tags []*tag
	err  bool
}

// Key implements proto.Node.
func (m *Msg) Key() string { return "adsc" }

// Text implements proto.Node.
func (m *Msg) Text(sb *strings.Builder, indent int) {
	c := &ctx{sb: sb, indent: indent}
	if len(m.tags) == 0 {
		c.printf("-- Empty ADS-C message\n")
		return
	}
	for _, t := range m.tags {
		if t.typ == nil {
			c.printf("-- Unparseable tag %d\n", t.id)
			continue
		}
		if t.typ.text != nil {
			t.typ.text(c, t.typ.label, t.data)
		}
	}
	if m.err {
		c.printf("-- Malformed ADS-C message\n")
	}
}

// JSON implements proto.Node.
func (m *Msg) JSON(w *ljson.W) {
	if len(m.tags) == 0 {
		return
	}
	c := &ctx{w: w}
	w.ArrayStart("tags")
	for _, t := range m.tags {
		if t.typ == nil {
			continue
		}
		if t.typ.json != nil && t.typ.key != "" {
			w.ObjectStart("")
			w.ObjectStart(t.typ.key)
			t.typ.json(c, t.data)
			w.ObjectEnd()
			w.ObjectEnd()
		}
	}
	w.ArrayEnd()
	w.Bool("err", m.err)
}

// Parse decodes an ADS-C payload. The direction picks the table of tags, since uplink and downlink use the same
// numbers for different things.
func Parse(buf []byte, dir fans.Dir, imi arinc.IMI) proto.Node {
	m := &Msg{}
	table := downlinkTags
	if dir == fans.Uplink {
		table = uplinkTags
	}
	switch imi {
	case arinc.ADS:
		for len(buf) > 0 {
			t := &tag{}
			m.tags = append(m.tags, t)
			n := parseTag(t, table, buf)
			if n < 0 {
				m.err = true
				break
			}
			buf = buf[n:]
		}
	case arinc.DIS:
		// The payload is a bare reason code. It is given a made-up tag, 255.
		if len(buf) < 1 {
			m.err = true
			break
		}
		t := &tag{}
		m.tags = append(m.tags, t)
		if parseTag(t, table, []byte{255, buf[0]}) < 0 {
			m.err = true
		}
	}
	return m
}

func parseTag(t *tag, table map[uint8]*typeDesc, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	t.id = buf[0]
	buf = buf[1:]
	typ := table[t.id]
	if typ == nil {
		return -1
	}
	n := 0
	if typ.parse != nil {
		if n = typ.parse(t, buf); n < 0 {
			return -1
		}
	}
	t.typ = typ
	return 1 + n
}

// ---- parsers ----

func parseUint8(t *tag, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	v := buf[0]
	t.data = &v
	return 1
}

func parseNack(t *tag, buf []byte) int {
	if len(buf) < 2 {
		return -1
	}
	n := &nack{contractReqNum: buf[0]}
	if buf[1] > 13 {
		return -1
	}
	n.reason = buf[1]
	tagLen := 2
	if buf[1] == 1 || buf[1] == 2 || buf[1] == 7 {
		tagLen++
		if len(buf) < tagLen {
			return -1
		}
		n.extData = buf[2]
	}
	t.data = n
	return tagLen
}

func parseNoncompGroup(g *noncompGroup, buf []byte) int {
	tagLen := 2
	if len(buf) < tagLen {
		return -1
	}
	g.tag = buf[0]
	g.unrecognized = buf[1]&0x80 != 0
	g.wholeGroupUnavl = buf[1]&0x40 != 0
	if g.unrecognized || g.wholeGroupUnavl {
		return tagLen
	}
	g.paramCnt = int(buf[1] & 0xf)
	if g.paramCnt == 0 {
		return tagLen
	}
	// The following octets hold 4-bit numbers of the non-compliant parameters.
	tagLen += g.paramCnt/2 + g.paramCnt%2
	if len(buf) < tagLen {
		return -1
	}
	p := buf[2:]
	for i := 0; i < g.paramCnt; i++ {
		g.params[i] = (p[0] >> (uint((i+1)%2) * 4)) & 0xf
		p = p[i%2:]
	}
	return tagLen
}

func parseNoncompNotify(t *tag, buf []byte) int {
	tagLen := 2
	if len(buf) < tagLen {
		return -1
	}
	n := &noncompNotify{contractReqNum: buf[0]}
	t.data = n
	cnt := int(buf[1])
	if cnt == 0 {
		return tagLen
	}
	n.groups = make([]noncompGroup, cnt)
	buf = buf[2:]
	for i := 0; i < cnt; i++ {
		used := parseNoncompGroup(&n.groups[i], buf)
		if used < 0 {
			return -1
		}
		buf = buf[used:]
		tagLen += used
		if len(buf) == 0 {
			if i < cnt-1 {
				return -1
			}
			break
		}
	}
	return tagLen
}

func parseBasicReport(t *tag, buf []byte) int {
	const tagLen = 10
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	r := &basicReport{}
	t.data = r
	r.lat = coordinate(b.read(21))
	r.lon = coordinate(b.read(21))
	r.alt = altitude(b.read(16))
	r.timestamp = timestamp(b.read(15))
	tmp := b.read(7)
	r.redundancy = uint8(tmp & 1)
	r.accuracy = uint8((tmp >> 1) & 0x7)
	r.tcas = uint8((tmp >> 4) & 1)
	return tagLen
}

func parseFlightID(t *tag, buf []byte) int {
	const tagLen = 6
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	id := make([]byte, 0, 8)
	for i := 0; i < 8; i++ {
		// ISO5 on 6 bits: space is 10 0000, letters 01 xxxx, digits 11 xxxx.
		tmp := b.read(6)
		if tmp&0x20 == 0 {
			tmp += 0x40
		}
		id = append(id, byte(tmp))
	}
	t.data = &flightID{string(id)}
	return tagLen
}

func parsePredictedRoute(t *tag, buf []byte) int {
	const tagLen = 17
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	r := &predictedRoute{}
	t.data = r
	r.latNext = coordinate(b.read(21))
	r.lonNext = coordinate(b.read(21))
	r.altNext = altitude(b.read(16))
	r.etaNext = int(b.read(14))
	r.latNextNext = coordinate(b.read(21))
	r.lonNextNext = coordinate(b.read(21))
	r.altNextNext = altitude(b.read(16))
	return tagLen
}

func parseEarthAirRef(t *tag, buf []byte) int {
	const tagLen = 5
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	r := &earthAirRef{}
	t.data = r
	r.headingInvalid = b.read(1) != 0
	r.heading = heading(b.read(12))
	r.speed = speed(b.read(13))
	r.vertSpeed = vertSpeed(b.read(12))
	return tagLen
}

func parseIntermediateProjection(t *tag, buf []byte) int {
	const tagLen = 8
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	p := &intermediateProjection{}
	t.data = p
	p.distance = distance(b.read(16))
	p.trackInvalid = b.read(1) != 0
	p.track = heading(b.read(12))
	p.alt = altitude(b.read(16))
	p.eta = int(b.read(14))
	return tagLen
}

func parseFixedProjection(t *tag, buf []byte) int {
	const tagLen = 9
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	p := &fixedProjection{}
	t.data = p
	p.lat = coordinate(b.read(21))
	p.lon = coordinate(b.read(21))
	p.alt = altitude(b.read(16))
	p.eta = int(b.read(14))
	return tagLen
}

func parseMeteo(t *tag, buf []byte) int {
	const tagLen = 4
	if len(buf) < tagLen {
		return -1
	}
	b := &bits{buf: buf[:tagLen]}
	m := &meteo{}
	t.data = m
	m.windSpeed = speed(b.read(9))
	m.windDirInvalid = b.read(1) != 0
	m.windDir = windDir(b.read(9))
	m.temp = temperature(b.read(12))
	return tagLen
}

func parseAirframeID(t *tag, buf []byte) int {
	if len(buf) < 3 {
		return -1
	}
	t.data = [3]byte{buf[0], buf[1], buf[2]}
	return 3
}

func parseReportingInterval(t *tag, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	sf := (buf[0] & 0xc0) >> 6
	switch sf {
	case 2:
		sf = 8
	case 3:
		sf = 64
	}
	t.data = &reportInterval{scalingFactor: sf, rate: buf[0] & 0x3f}
	return 1
}

func parseLatDevChange(t *tag, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	v := float64(buf[0]) / 8.0
	t.data = &v
	return 1
}

func parseVspdChange(t *tag, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	v := signExtend(uint32(buf[0]), 8) * 64
	t.data = &v
	return 1
}

func parseAltRange(t *tag, buf []byte) int {
	if len(buf) < 4 {
		return -1
	}
	t.data = &altRange{
		ceiling: altitude(uint32(buf[0])<<8 | uint32(buf[1])),
		floor:   altitude(uint32(buf[2])<<8 | uint32(buf[3])),
	}
	return 4
}

func parseAcftIntentGroup(t *tag, buf []byte) int {
	if len(buf) < 2 {
		return -1
	}
	t.data = &acftIntentReq{modulus: buf[0], projTime: buf[1]}
	return 2
}

func parseContractRequest(t *tag, buf []byte) int {
	if len(buf) < 1 {
		return -1
	}
	r := &request{contractNum: buf[0]}
	t.data = r
	tagLen := 1
	buf = buf[1:]
	for len(buf) > 0 {
		// A tag that is not a request tag is taken to start the next request of a multi-request message.
		if requestTags[buf[0]] == nil {
			break
		}
		rt := &tag{}
		r.tags = append(r.tags, rt)
		used := parseTag(rt, requestTags, buf)
		if used < 0 {
			return -1
		}
		buf = buf[used:]
		tagLen += used
	}
	return tagLen
}
