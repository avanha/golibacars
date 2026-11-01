package fans

import (
	"fmt"

	"github.com/avanha/golibacars/per"
)

// unit describes a formatter that prints an integer scaled by mult, followed by a unit.
type unit struct {
	name string
	mult float64
	dec  int
}

// output formats p.v with the formatter its type has in table (la_asn1_output). Types with no
// formatter are skipped, or, for text, dumped, when dump is set.
func output(caller *params, table map[*per.Type]entry, dump bool) {
	// The C code passes its parameters by value, so a formatter may change the indent for its children only.
	cp := *caller
	p := &cp
	e, ok := table[p.v.Type]
	if !ok {
		if dump {
			p.isprintf(p.indent, "-- Formatter for type %s not found, ASN.1 dump follows:\n", p.v.Type.Name)
			p.isprintf(p.indent, "%s", "")
			p.text.WriteString("<" + p.v.Type.Name + ">")
			p.text.WriteString("\n")
			p.isprintf(p.indent, "%s", "-- ASN.1 dump end\n")
		}
		return
	}
	if e.fn == "" {
		return
	}
	p.label, p.hasLbl = e.label, e.has
	if p.json != nil {
		formatJSON(p, e.fn)
	} else {
		formatText(p, e.fn)
	}
}

// member returns the value of the i-th member of a SEQUENCE or CHOICE value, or nil if it is absent.
func named(v *per.Value, name string) *per.Value {
	for i, m := range v.Type.Members {
		if m.Name == name {
			return v.Members[i]
		}
	}
	return nil
}

// enumName is la_asn1_value2enum: the name of an enumerated value, or "" if there is none.
func enumName(t *per.Type, v int64) (string, bool) { return t.EnumName(v) }

func formatText(p *params, fn string) {
	switch fn {
	case "SEQUENCE_cpdlc":
		textSequence(p)
	case "CHOICE_cpdlc":
		textChoice(p, nil)
	case "SEQUENCE_OF_cpdlc":
		textSequenceOf(p)
	case "FANSATCUplinkMsgElementId":
		textChoice(p, uplinkLabels)
	case "FANSATCDownlinkMsgElementId":
		textChoice(p, downlinkLabels)
	case "FANSATCUplinkMessage", "FANSATCDownlinkMessage":
		textMessage(p)
	case "any":
		textAny(p)
	case "ENUM":
		if s, ok := enumName(p.v.Type, p.v.Int); ok {
			p.isprintf(p.indent, "%s: %s\n", p.label, s)
		} else {
			p.isprintf(p.indent, "%s: %d\n", p.label, p.v.Int)
		}
	case "FANSBeaconCode":
		it := p.v.Items
		p.isprintf(p.indent, "%s: %d%d%d%d\n", p.label, it[0].Int, it[1].Int, it[2].Int, it[3].Int)
	case "FANSTime":
		p.isprintf(p.indent, "%s: %02d:%02d\n", p.label, named(p.v, "hours").Int, named(p.v, "minutes").Int)
	case "FANSTimestamp":
		p.isprintf(p.indent, "%s: %02d:%02d:%02d\n", p.label,
			named(p.v, "hours").Int, named(p.v, "minutes").Int, named(p.v, "seconds").Int)
	case "FANSLatitude":
		textLatLon(p, "latitude", "%s:   %02d %04.1f' %s\n", "%s:   %02d deg %s\n")
	case "FANSLongitude":
		textLatLon(p, "longitude", "%s: %03d %04.1f' %s\n", "%s: %03d deg %s\n")
	default:
		u, ok := textUnits[fn]
		if !ok {
			panic("fans: no text formatter " + fn)
		}
		p.isprintf(p.indent, "%s: %.*f%s\n", p.label, u.dec, float64(p.v.Int)*u.mult, u.name)
	}
}

// textLatLon prints a latitude or longitude with the layout libacars gives each (they differ in spacing).
func textLatLon(p *params, what, withMin, noMin string) {
	deg := named(p.v, what+"Degrees").Int
	dv := named(p.v, what+"Direction")
	dir, _ := enumName(dv.Type, dv.Int)
	if mv := named(p.v, "minutesLatLon"); mv != nil {
		p.isprintf(p.indent, withMin, p.label, deg, float64(mv.Int)/10.0, dir)
	} else {
		p.isprintf(p.indent, noMin, p.label, deg, dir)
	}
}

func textAny(p *params) {
	if p.hasLbl {
		p.isprintf(p.indent, "%s: ", p.label)
	} else {
		p.isprintf(p.indent, "%s", "")
	}
	p.text.WriteString(printValue(p.v))
	p.text.WriteString("\n")
}

// printValue is asn_sprintf for the simple types.
func printValue(v *per.Value) string {
	switch v.Type.Kind {
	case per.IA5String, per.NumericString:
		return string(v.Str)
	case per.Integer:
		return fmt.Sprintf("%d", v.Int)
	case per.Boolean:
		if v.Bool {
			return "TRUE"
		}
		return "FALSE"
	}
	panic("fans: cannot print " + v.Type.Name)
}

func textSequence(p *params) {
	if p.hasLbl {
		p.isprintf(p.indent, "%s:\n", p.label)
		p.indent++
	}
	q := *p
	for _, m := range p.v.Members {
		if m == nil {
			continue
		}
		q.v = m
		output(&q, textTable, true)
	}
}

func textSequenceOf(p *params) {
	if p.hasLbl {
		p.isprintf(p.indent, "%s:\n", p.label)
		p.indent++
	}
	for _, it := range p.v.Items {
		q := *p
		q.v = it
		output(&q, textTable, true)
	}
}

func textChoice(p *params, labels map[int]string) {
	present := p.v.Choice + 1
	if p.hasLbl {
		p.isprintf(p.indent, "%s:\n", p.label)
		p.indent++
	}
	if labels != nil {
		if d, ok := labels[p.v.Choice]; ok {
			p.isprintf(p.indent, "%s\n", d)
		} else {
			p.isprintf(p.indent, "<no description for CHOICE value %d>\n", present)
		}
		p.indent++
	}
	q := *p
	q.v = p.v.Members[0]
	output(&q, textTable, true)
}

// textMessage prints an uplink or downlink PDU: header, then the message elements under "Message data".
func textMessage(p *params) {
	if p.hasLbl {
		p.isprintf(p.indent, "%s:\n", p.label)
		p.indent++
	}
	m := p.v.Members
	q := *p
	q.v = m[0]
	output(&q, textTable, true)
	p.isprintf(p.indent, "Message data:\n")
	p.indent++
	q = *p
	q.v = m[1]
	output(&q, textTable, true)
	if m[2] != nil {
		q = *p
		q.v = m[2]
		output(&q, textTable, true)
	}
}

func formatJSON(p *params, fn string) {
	w := p.json
	switch fn {
	case "SEQUENCE_cpdlc":
		w.ObjectStart(p.label)
		q := *p
		for _, m := range p.v.Members {
			if m != nil {
				q.v = m
				output(&q, jsonTable, false)
			}
		}
		w.ObjectEnd()
	case "CHOICE_cpdlc":
		jsonChoice(p, nil)
	case "SEQUENCE_OF_cpdlc":
		w.ArrayStart(p.label)
		for _, it := range p.v.Items {
			w.ObjectStart("")
			q := *p
			q.v = it
			output(&q, jsonTable, false)
			w.ObjectEnd()
		}
		w.ArrayEnd()
	case "FANSATCUplinkMsgElementId":
		jsonChoice(p, uplinkLabels)
	case "FANSATCDownlinkMsgElementId":
		jsonChoice(p, downlinkLabels)
	case "any_as_string":
		w.Bytes(p.label, []byte(printValue(p.v)))
	case "long":
		w.Int64(p.label, p.v.Int)
	case "bool":
		w.Bool(p.label, p.v.Bool)
	case "ENUM":
		if s, ok := enumName(p.v.Type, p.v.Int); ok {
			w.Str(p.label, s)
		} else {
			w.Int64(p.label, p.v.Int)
		}
	case "FANSBeaconCode":
		it := p.v.Items
		w.Str(p.label, string([]byte{byte(it[0].Int) + '0', byte(it[1].Int) + '0', byte(it[2].Int) + '0', byte(it[3].Int) + '0'}))
	case "FANSTime":
		w.ObjectStart(p.label)
		w.Int64("hour", named(p.v, "hours").Int)
		w.Int64("min", named(p.v, "minutes").Int)
		w.ObjectEnd()
	case "FANSTimestamp":
		w.ObjectStart(p.label)
		w.Int64("hour", named(p.v, "hours").Int)
		w.Int64("min", named(p.v, "minutes").Int)
		w.Int64("sec", named(p.v, "seconds").Int)
		w.ObjectEnd()
	case "FANSLatitude", "FANSLongitude":
		what := "latitude"
		if fn == "FANSLongitude" {
			what = "longitude"
		}
		w.ObjectStart(p.label)
		w.Int64("deg", named(p.v, what+"Degrees").Int)
		if mv := named(p.v, "minutesLatLon"); mv != nil {
			w.Double("min", float64(mv.Int)/10.0)
		}
		d := named(p.v, what+"Direction")
		name, _ := enumName(d.Type, d.Int)
		w.Str("dir", name)
		w.ObjectEnd()
	default:
		u, ok := jsonUnits[fn]
		if !ok {
			panic("fans: no JSON formatter " + fn)
		}
		w.ObjectStart(p.label)
		w.Double("val", float64(p.v.Int)*u.mult)
		w.Str("unit", u.name)
		w.ObjectEnd()
	}
}

func jsonChoice(p *params, labels map[int]string) {
	w := p.json
	w.ObjectStart(p.label)
	if labels != nil {
		w.Str("choice_label", labels[p.v.Choice])
	}
	w.Str("choice", p.v.Type.Members[p.v.Choice].Name)
	w.ObjectStart("data")
	q := *p
	q.v = p.v.Members[0]
	output(&q, jsonTable, false)
	w.ObjectEnd()
	w.ObjectEnd()
}
