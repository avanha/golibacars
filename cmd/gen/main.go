// Command gen produces the FANS-1/A tables in package fans from the C that libacars ships:
// the asn1c output in libacars/asn1 and the formatter tables in asn1-format-cpdlc-*.c.
//
// libacars does not include the ASN.1 source, so the generated C is the only description of the
// types. The output is checked in; run this only to refresh it from a new libacars:
//
//	go run ./cmd/gen /path/to/libacars/libacars fans
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen <libacars/libacars dir> <output dir>")
		os.Exit(2)
	}
	src, out := os.Args[1], os.Args[2]
	g := &gen{src: src, constraints: map[string]*constraint{}, types: map[string]*typeDef{}}
	g.readTypes()
	write(filepath.Join(out, "types_gen.go"), g.emitTypes())
	write(filepath.Join(out, "formats_gen.go"), g.emitFormats())
}

func write(path string, b []byte) {
	f, err := format.Source(b)
	if err != nil {
		os.WriteFile(path, b, 0o644)
		fatal("gofmt %s: %v", path, err)
	}
	if err := os.WriteFile(path, f, 0o644); err != nil {
		fatal("%v", err)
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+f+"\n", a...)
	os.Exit(1)
}

type constraint struct {
	name          string
	value, size   [5]int64 // flags, range bits, effective bits, lower, upper
	code2valueTbl []int
}

type member struct {
	name     string
	optional bool
	typ      string
	per      string // name of a constraint, or ""
}

type enumEntry struct {
	value int64
	name  string
}

type typeDef struct {
	name    string
	kind    string // per.Sequence ...
	per     string
	members []member
	enum    []enumEntry
	rootOpt int
	prNames []string // CHOICE: the _PR_ suffixes, in member order
	aliasOf string
}

type gen struct {
	src         string
	constraints map[string]*constraint
	types       map[string]*typeDef
}

var (
	reComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reDef     = regexp.MustCompile(`(?s)asn_TYPE_descriptor_t asn_DEF_(\w+) = \{(.*?)\n\};`)
	reMembers = regexp.MustCompile(`(?s)static asn_TYPE_member_t asn_MBR_(\w+)\[\] = \{(.*?)\n\};`)
	reCons    = regexp.MustCompile(`(?s)static (?:const )?asn_per_constraints_t (\w+)(?: GCC_NOTUSED)? = \{(.*?)\n\};`)
	reEnum    = regexp.MustCompile(`(?s)static const asn_INTEGER_enum_map_t (\w+)\[\] = \{(.*?)\n\};`)
	reSpecs   = regexp.MustCompile(`(?s)static (?:const )?asn_(\w+)_specifics_t (\w+) = \{(.*?)\n\};`)
	reInherit = regexp.MustCompile(`td->uper_decoder\s*=\s*asn_DEF_(\w+)\.uper_decoder`)
	reArray   = regexp.MustCompile(`(?s)static const int (permitted_alphabet_code2value_\d+)\[\d+\] = \{(.*?)\};`)
)

// splitTop splits s on commas that are not inside brackets or quotes.
func splitTop(s string) []string {
	var parts []string
	depth, start, inq := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inq:
			if c == '\\' {
				i++
			} else if c == '"' {
				inq = false
			}
		case c == '"':
			inq = true
		case c == '(' || c == '{' || c == '[':
			depth++
		case c == ')' || c == '}' || c == ']':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		parts = append(parts, t)
	}
	return parts
}

func (g *gen) readFile(name string) string {
	b, err := os.ReadFile(filepath.Join(g.src, name))
	if err != nil {
		fatal("%v", err)
	}
	return string(b)
}

func atoi(s string) int64 {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		n, err := strconv.ParseInt(s[2:], 16, 64)
		if err != nil {
			fatal("number %q", s)
		}
		return n
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fatal("number %q", s)
	}
	return n
}

func (g *gen) readTypes() {
	files, _ := filepath.Glob(filepath.Join(g.src, "asn1", "FANS*.c"))
	sort.Strings(files)
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		text := reComment.ReplaceAllString(string(raw), "")
		for _, m := range reCons.FindAllStringSubmatch(text, -1) {
			g.parseConstraint(m[1], m[2], text)
		}
		d := reDef.FindStringSubmatch(text)
		if d == nil {
			continue // a helper file
		}
		g.parseType(d[1], d[2], text)
	}
	if len(g.types) < 200 {
		fatal("only %d types found", len(g.types))
	}
	for _, t := range g.types {
		g.resolve(t)
	}
}

// resolve fills in a type defined as another FANS type with what it inherits from it.
func (g *gen) resolve(t *typeDef) {
	if t.kind != "" {
		return
	}
	b, ok := g.types[t.aliasOf]
	if !ok {
		fatal("type %s: base %s not found", t.name, t.aliasOf)
	}
	g.resolve(b)
	t.kind = b.kind
	if t.per == "" {
		t.per = b.per
	}
	if len(t.members) == 0 {
		t.members, t.rootOpt, t.prNames = b.members, b.rootOpt, b.prNames
	}
	if len(t.enum) == 0 {
		t.enum = b.enum
	}
}

func (g *gen) parseConstraint(name, body, text string) {
	c := &constraint{name: strings.TrimPrefix(name, "asn_PER_")}
	parts := splitTop(body)
	if len(parts) != 4 {
		fatal("constraint %s: %d parts", name, len(parts))
	}
	parse := func(s string) (r [5]int64) {
		f := splitTop(strings.Trim(strings.TrimSpace(s), "{}"))
		if len(f) != 5 {
			fatal("constraint %s: %q", name, s)
		}
		for i, x := range f {
			x = strings.TrimSpace(x)
			switch x {
			case "APC_UNCONSTRAINED":
				r[i] = 0
			case "APC_SEMI_CONSTRAINED":
				r[i] = 1
			case "APC_CONSTRAINED":
				r[i] = 2
			case "APC_CONSTRAINED | APC_EXTENSIBLE":
				r[i] = 6
			default:
				r[i] = atoi(x)
			}
		}
		return
	}
	c.value, c.size = parse(parts[0]), parse(parts[1])
	if c2v := strings.TrimSpace(parts[3]); c2v != "0" {
		fn := regexp.MustCompile(`(?s)static int ` + regexp.QuoteMeta(c2v) + `\(.*?return (permitted_alphabet_code2value_\d+)\[code\]`).FindStringSubmatch(text)
		if fn == nil {
			fatal("constraint %s: cannot find the code-to-value table %s", name, c2v)
		}
		arr := reArray.FindStringSubmatch(text)
		if arr == nil || arr[1] != fn[1] {
			fatal("constraint %s: table %s not found", name, fn[1])
		}
		for _, x := range strings.Split(arr[2], ",") {
			if x = strings.TrimSpace(x); x != "" {
				c.code2valueTbl = append(c.code2valueTbl, int(atoi(x)))
			}
		}
	}
	g.constraints[c.name] = c
}

func (g *gen) parseType(name, body, text string) {
	t := &typeDef{name: name}
	f := splitTop(body)
	if len(f) < 20 {
		fatal("type %s: %d descriptor fields", name, len(f))
	}
	dec := strings.TrimSuffix(f[9], "_decode_uper")
	base := dec
	if strings.HasPrefix(dec, "FANS") {
		m := reInherit.FindStringSubmatch(text)
		if m == nil {
			fatal("type %s: no base type", name)
		}
		base = m[1]
		t.aliasOf = base
	}
	switch base {
	case "SEQUENCE":
		t.kind = "per.Sequence"
	case "CHOICE":
		t.kind = "per.Choice"
	case "SEQUENCE_OF":
		t.kind = "per.SequenceOf"
	case "NativeInteger":
		t.kind = "per.Integer"
	case "NativeEnumerated":
		t.kind = "per.Enumerated"
	case "NULL":
		t.kind = "per.Null"
	case "BOOLEAN":
		t.kind = "per.Boolean"
	case "IA5String":
		t.kind = "per.IA5String"
	case "NumericString":
		t.kind = "per.NumericString"
	default:
		if !strings.HasPrefix(base, "FANS") {
			fatal("type %s: unsupported base %s", name, base)
		}
		t.kind = "" // another FANS type; filled in by resolve
	}
	if p := f[16]; p != "0" {
		t.per = strings.TrimPrefix(strings.TrimPrefix(p, "&asn_PER_"), "")
		if _, ok := g.constraints[t.per]; !ok {
			fatal("type %s: constraints %s not found", name, p)
		}
	}
	// Members.
	if f[17] != "0" {
		mb := reMembers.FindStringSubmatch(text)
		if mb == nil {
			fatal("type %s: members %s not found", name, f[17])
		}
		for _, mtext := range splitTop(mb[2]) {
			mf := splitTop(strings.Trim(strings.TrimSpace(mtext), "{}"))
			if len(mf) != 10 {
				fatal("type %s: member with %d fields: %q", name, len(mf), mtext)
			}
			m := member{
				optional: atoi(mf[1]) != 0,
				typ:      strings.TrimPrefix(mf[5], "&asn_DEF_"),
				name:     strings.Trim(mf[9], `"`),
			}
			if mf[7] != "0" {
				m.per = strings.TrimPrefix(mf[7], "&asn_PER_")
				if _, ok := g.constraints[m.per]; !ok {
					fatal("type %s member %s: constraints %s not found", name, m.name, mf[7])
				}
			}
			t.members = append(t.members, m)
		}
		if n := atoi(f[18]); int(n) != len(t.members) {
			fatal("type %s: %d members, descriptor says %d", name, len(t.members), n)
		}
	}
	// Specifics.
	if f[19] != "0" {
		want := strings.TrimPrefix(f[19], "&")
		var sp []string
		var kindName string
		for _, s := range reSpecs.FindAllStringSubmatch(text, -1) {
			if s[2] == want {
				kindName, sp = s[1], splitTop(s[3])
			}
		}
		if sp == nil {
			fatal("type %s: specifics %s not found", name, want)
		}
		switch kindName {
		case "SEQUENCE":
			// struct size, ctx, tag map, tag count, optional members map, roots, additions, ext before, ext after
			t.rootOpt = int(atoi(sp[5]))
			if atoi(sp[6]) != 0 || atoi(sp[7]) != -1 || atoi(sp[8]) != -1 {
				fatal("type %s: extensions are not supported", name)
			}
		case "CHOICE":
			// struct size, ctx, present offset, present size, tag map, tag count, canonical order, ext start
			if sp[6] != "0" || atoi(sp[7]) != -1 {
				fatal("type %s: choice extensions or reordering are not supported", name)
			}
		case "SET_OF":
			// struct size, ctx offset, XER cmap: nothing to keep
		case "INTEGER":
			// value-to-enum map, enum-to-value map, count, extension, strict, field width, unsigned
			if sp[0] != "0" {
				for _, e := range reEnum.FindAllStringSubmatch(text, -1) {
					if e[1] != sp[0] {
						continue
					}
					for _, ent := range splitTop(e[2]) {
						ef := splitTop(strings.Trim(strings.TrimSpace(ent), "{}"))
						t.enum = append(t.enum, enumEntry{atoi(ef[0]), strings.Trim(ef[2], `"`)})
					}
				}
				if len(t.enum) != int(atoi(sp[2])) {
					fatal("type %s: %d enum entries, specifics say %s", name, len(t.enum), sp[2])
				}
				if atoi(sp[3]) != 0 {
					fatal("type %s: extensible enumeration", name)
				}
			}
		default:
			fatal("type %s: unknown specifics %s", name, kindName)
		}
	}
	// CHOICE member constants, used to attach libacars' labels to members.
	if t.kind == "per.Choice" {
		h, _ := os.ReadFile(filepath.Join(g.src, "asn1", name+".h"))
		re := regexp.MustCompile(name + `_PR_(\w+)`)
		for _, m := range re.FindAllStringSubmatch(string(h), -1) {
			if m[1] != "NOTHING" && !contains(t.prNames, m[1]) {
				t.prNames = append(t.prNames, m[1])
			}
		}
	}
	g.types[name] = t
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func goName(n string) string { return "t" + n }

func (g *gen) emitConstraint(b *bytes.Buffer, c *constraint) {
	lit := func(v [5]int64) string {
		return fmt.Sprintf("per.Constraint{Flags: %d, RangeBits: %d, EffBits: %d, Lower: %d, Upper: %d}", v[0], v[1], v[2], v[3], v[4])
	}
	fmt.Fprintf(b, "var c_%s = &per.Constraints{Value: %s, Size: %s", c.name, lit(c.value), lit(c.size))
	if c.code2valueTbl != nil {
		fmt.Fprintf(b, ", Code2Value: func(code uint32) int {\n\ttbl := %#v\n\tif int(code) >= len(tbl) {\n\t\treturn -1\n\t}\n\treturn tbl[code]\n}", c.code2valueTbl)
	}
	b.WriteString("}\n")
}

func (g *gen) emitTypes() []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/gen from the asn1c output in libacars. DO NOT EDIT.\n\npackage fans\n\nimport \"github.com/avanha/golibacars/per\"\n\n")
	var cn []string
	for n := range g.constraints {
		cn = append(cn, n)
	}
	sort.Strings(cn)
	for _, n := range cn {
		g.emitConstraint(&b, g.constraints[n])
	}
	// Library types that members refer to directly.
	base := map[string]string{"NULL": "per.Null", "BOOLEAN": "per.Boolean", "NativeInteger": "per.Integer",
		"NativeEnumerated": "per.Enumerated", "IA5String": "per.IA5String", "NumericString": "per.NumericString"}
	used := map[string]bool{}
	for _, t := range g.types {
		for _, m := range t.members {
			if _, ok := g.types[m.typ]; !ok {
				if base[m.typ] == "" {
					fatal("type %s member %s: unknown type %s", t.name, m.name, m.typ)
				}
				used[m.typ] = true
			}
		}
	}
	var un []string
	for n := range used {
		un = append(un, n)
	}
	sort.Strings(un)
	for _, n := range un {
		fmt.Fprintf(&b, "\nvar %s = &per.Type{Name: %q, Kind: %s}\n", goName(n), n, base[n])
	}
	var tn []string
	for n := range g.types {
		tn = append(tn, n)
	}
	sort.Strings(tn)
	for _, n := range tn {
		t := g.types[n]
		fmt.Fprintf(&b, "\nvar %s = &per.Type{Name: %q, Kind: %s", goName(n), n, t.kind)
		if t.per != "" {
			fmt.Fprintf(&b, ", Per: c_%s", t.per)
		}
		if t.rootOpt != 0 {
			fmt.Fprintf(&b, ", RootOptional: %d", t.rootOpt)
		}
		if len(t.enum) > 0 {
			b.WriteString(", Enum: []per.EnumEntry{")
			for _, e := range t.enum {
				fmt.Fprintf(&b, "{Value: %d, Name: %q}, ", e.value, e.name)
			}
			b.WriteString("}")
		}
		if len(t.members) > 0 {
			b.WriteString(", Members: []per.Member{\n")
			for _, m := range t.members {
				fmt.Fprintf(&b, "\t{Name: %q, Type: %s", m.name, goName(m.typ))
				if m.optional {
					b.WriteString(", Optional: true")
				}
				if m.per != "" {
					fmt.Fprintf(&b, ", Per: c_%s", m.per)
				}
				b.WriteString("},\n")
			}
			b.WriteString("}")
		}
		b.WriteString("}\n")
	}
	// CHOICE member names, used to find the label of a message element.
	return b.Bytes()
}

var (
	reTable = regexp.MustCompile(`(?m)^\t\{ \.type = &asn_DEF_(\w+), \.format = (\w+), \.label = (NULL|"[^"]*") \}`)
	reDict  = regexp.MustCompile(`(?m)^\t\{ (\w+)_PR_(\w+), "([^"]*)" \}`)
	reUnit  = regexp.MustCompile(`(?s)(?:static )?LA_ASN1_FORMATTER_FUNC\((\w+)\) \{\s*la_format_INTEGER_with_unit_as_(text|json)\(p, "([^"]*)", ([0-9.]+)(?:, (\d+))?\);\s*\}`)
)

func (g *gen) emitFormats() []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/gen from libacars' CPDLC formatter tables. DO NOT EDIT.\n\npackage fans\n\n")
	b.WriteString("import \"github.com/avanha/golibacars/per\"\n\n")
	b.WriteString("// entry is one row of a formatter table: which formatter handles a type, and its label.\n")
	b.WriteString("type entry struct {\n\tfn    string\n\tlabel string\n\thas   bool // the label is set (a nil label in C)\n}\n\n")

	for _, mode := range []string{"text", "json"} {
		text := g.readFile("asn1-format-cpdlc-" + mode + ".c")
		fmt.Fprintf(&b, "var %sTable = map[*per.Type]entry{\n", mode)
		n := 0
		for _, m := range reTable.FindAllStringSubmatch(text, -1) {
			if m[1] == "NULL" {
				// A NULL value is deliberately printed as nothing; the entry stops it being reported as unknown.
				fmt.Fprintf(&b, "\ttNULL: {},\n")
				continue
			}
			if _, ok := g.types[m[1]]; !ok {
				fatal("%s table: unknown type %s", mode, m[1])
			}
			fn := strings.TrimPrefix(m[2], "la_asn1_format_")
			fn = strings.TrimSuffix(fn, "_as_"+mode)
			label, has := "", m[3] != "NULL"
			if has {
				label = strings.Trim(m[3], `"`)
			}
			fmt.Fprintf(&b, "\t%s: {%q, %q, %v},\n", goName(m[1]), fn, label, has)
			n++
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "// %sUnits holds the formatters that print an integer scaled, with a unit.\n", mode)
		fmt.Fprintf(&b, "var %sUnits = map[string]unit{\n", mode)
		for _, u := range reUnit.FindAllStringSubmatch(text, -1) {
			if u[2] != mode {
				continue
			}
			fn := strings.TrimSuffix(strings.TrimPrefix(u[1], "la_asn1_format_"), "_as_"+mode)
			dec := "0"
			if u[5] != "" {
				dec = u[5]
			}
			fmt.Fprintf(&b, "\t%q: {%q, %s, %s},\n", fn, u[3], u[4], dec)
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "// (%d formatter entries)\n\n", n)
	}

	// Labels of the message elements.
	ctext := g.readFile("asn1-format-cpdlc-text.c")
	for _, dir := range []string{"Uplink", "Downlink"} {
		tn := "FANSATC" + dir + "MsgElementId"
		t := g.types[tn]
		labels := map[string]string{}
		for _, m := range reDict.FindAllStringSubmatch(ctext, -1) {
			if m[1] == tn {
				labels[m[2]] = m[3]
			}
		}
		fmt.Fprintf(&b, "// %sLabels names the %d message elements of the %s, by member index.\n", strings.ToLower(dir), len(labels), tn)
		fmt.Fprintf(&b, "var %sLabels = map[int]string{\n", strings.ToLower(dir))
		for i, pr := range t.prNames {
			if l, ok := labels[pr]; ok {
				fmt.Fprintf(&b, "\t%d: %q,\n", i, l)
			}
		}
		b.WriteString("}\n\n")
		if len(t.prNames) != len(t.members) {
			fatal("%s: %d PR constants, %d members", tn, len(t.prNames), len(t.members))
		}
	}
	return b.Bytes()
}
