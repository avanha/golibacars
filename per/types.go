package per

// Constraint flags, as in asn1c's per_support.h.
const (
	Unconstrained   = 0x0
	SemiConstrained = 0x1
	Constrained     = 0x2
	Extensible      = 0x4
)

// Constraint is one PER-visible constraint (asn_per_constraint_t).
type Constraint struct {
	Flags     int
	RangeBits int // bits for a value in the range, or -1
	EffBits   int // bits for a length or index in the range, or -1
	Lower     int64
	Upper     int64
}

// Constraints holds the value and size constraints of a type or member (asn_per_constraints_t).
type Constraints struct {
	Value Constraint
	Size  Constraint
	// Code2Value maps an encoded character code to its value for a permitted alphabet; nil if there is none.
	Code2Value func(code uint32) int
}

// Kind is the ASN.1 construct a type decodes as.
type Kind uint8

const (
	Sequence Kind = iota + 1
	Choice
	SequenceOf
	Integer
	Enumerated
	Null
	Boolean
	IA5String
	NumericString
)

// EnumEntry is one named value of an ENUMERATED or INTEGER type.
type EnumEntry struct {
	Value int64
	Name  string
}

// Member is one component of a SEQUENCE, a CHOICE, or the element of a SEQUENCE OF.
type Member struct {
	Name     string
	Optional bool
	Type     *Type
	Per      *Constraints // constraints on this use of the type; they replace the type's own
}

// Type describes an ASN.1 type.
type Type struct {
	Name    string
	Kind    Kind
	Per     *Constraints
	Members []Member
	Enum    []EnumEntry // value-to-name map, in encoding order
	// RootOptional is the number of optional members of a SEQUENCE (the length of the presence bitmap).
	RootOptional int
}

// Value is a decoded value. Which fields are set depends on Type.Kind.
type Value struct {
	Type    *Type
	Int     int64    // Integer, Enumerated; the member's value
	Bool    bool     // Boolean
	Str     []byte   // IA5String, NumericString
	Choice  int      // Choice: index of the member present
	Members []*Value // Sequence: one per member, nil when an optional member is absent; Choice: the one member at Choice
	Items   []*Value // SequenceOf
}

// EnumName returns the name of an integer value in an ENUMERATED type, and false if there is none.
func (t *Type) EnumName(v int64) (string, bool) {
	for _, e := range t.Enum {
		if e.Value == v {
			return e.Name, true
		}
	}
	return "", false
}
