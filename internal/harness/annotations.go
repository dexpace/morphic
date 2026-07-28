package harness

// Annotation names one annotation slot the IR offers at a site. It is test
// vocabulary: nothing in the compiler's write path refers to these values, and
// ir.Diagnostic carries no annotation field. Annotation is one axis of
// annotation retention; SiteKind is the other.
//
// The name is broader here than in JSON Schema Core, which recognizes four
// keyword categories, not a two-way split: identifiers ($id, $schema),
// applicators (properties, allOf, not, if/then/else, dependentSchemas,
// contains, unevaluated* — apply a subschema rather than assert against the
// instance directly), assertions (type, minimum, pattern), and annotations in
// the spec's own narrower sense (title, default, examples: no validation
// effect). AnnotationConstraints corresponds to the assertions category;
// AnnotationValidationOnly names applicator keywords with no structural IR
// home (preserved verbatim per ir-design §4.7) — applicators, not assertions,
// despite the stricter term. This suite groups everything under one
// Annotation name anyway because the IR attaches each at a source position
// rather than using it to define the type there, which is the distinction
// retention actually measures.
type Annotation string

// The annotation slots the IR offers. Adding a slot here widens what
// annotation retention checks, which is the intended way to discover that a
// format handles it nowhere — TestConstBlock_TiesToAnnotationsAndSiteKinds
// (annotations_test.go) requires every constant declared below to also
// appear in Annotations(), so a slot added here without also being added
// there fails that test rather than sitting unused.
const (
	AnnotationDocs           Annotation = "docs"
	AnnotationExamples       Annotation = "examples"
	AnnotationConstraints    Annotation = "constraints"
	AnnotationDefault        Annotation = "default"
	AnnotationDeprecated     Annotation = "deprecated"
	AnnotationVisibility     Annotation = "visibility"
	AnnotationExtensions     Annotation = "extensions"
	AnnotationXMLHints       Annotation = "xmlHints"
	AnnotationValidationOnly Annotation = "validationOnly"
)

// SiteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own. Declaration
// splits by the declaring component's own shape because the compiler routes an
// object-shaped body and a scalar-shaped body through different lowering paths
// that do not honor the same keywords: an annotation present on one shape is
// not evidence it survives on the other. The distinction is annotation
// retention's second axis because this is where annotations are most often
// dropped.
//
// SiteDeclarationModel and SiteDeclarationScalar do not cover every
// declaration shape the OpenAPI compiler's schema dispatch (lower(), in
// schema.go) produces. Six other destinations exist: const (hoistLiteral ->
// Literal), enum (lowerEnum -> Enum, or a Union of Literals when
// heterogeneous), a multi-type such as `type: [string, integer]` (lowerUnion
// -> Union), a `type: array` body (lowerArray -> List/Tuple), allOf
// (lowerAllOf -> Model), and oneOf/anyOf (lowerOneOfAnyOf -> Union when it
// has no structural siblings; otherwise it is hoisted through whichever of
// the other five destinations its sibling shape implies, then has the raw
// oneOf/anyOf preserved under Extensions on top — see lowerWithUnionSiblings).
//
// Of these six, only lowerAllOf builds a Model and calls fillModelDetail, the
// same as lowerModel: a docs/deprecated/x-* annotation on an allOf-composed
// component is retained, not dropped, even though this grid does not
// exercise allOf as its own SiteKind. hoistLiteral, lowerEnum, lowerUnion,
// and lowerArray never call fillModelDetail, so an annotation on a const,
// enum, multi-type, or array declaration is believed to drop the same way
// SiteDeclarationScalar's cases do — but, unlike allOf, that belief is
// untested here, not verified.
type SiteKind string

// The kinds of position an annotation can be written at.
// TestConstBlock_TiesToAnnotationsAndSiteKinds requires every constant below
// to appear in SiteKinds(), the same way it does for Annotations() above.
const (
	// SiteDeclarationModel is an annotation on an object-shaped component,
	// which lowers through the compiler's model path.
	SiteDeclarationModel SiteKind = "declaration-model"
	// SiteDeclarationScalar is an annotation on a scalar-shaped component,
	// which lowers through the compiler's alias path.
	SiteDeclarationScalar SiteKind = "declaration-scalar"
	// SiteReference is an annotation written beside a $ref.
	SiteReference SiteKind = "reference"
)

// Cell identifies one annotation at one kind of position: the unit annotation
// retention is checked at.
type Cell struct {
	// Annotation is the annotation slot this cell checks.
	Annotation Annotation
	// SiteKind is the position kind this cell checks Annotation at.
	SiteKind SiteKind
}

// Annotations returns every annotation in a stable order.
func Annotations() []Annotation {
	return []Annotation{
		AnnotationDocs,
		AnnotationExamples,
		AnnotationConstraints,
		AnnotationDefault,
		AnnotationDeprecated,
		AnnotationVisibility,
		AnnotationExtensions,
		AnnotationXMLHints,
		AnnotationValidationOnly,
	}
}

// SiteKinds returns every site kind in a stable order.
func SiteKinds() []SiteKind {
	return []SiteKind{SiteDeclarationModel, SiteDeclarationScalar, SiteReference}
}

// Cells returns the full annotation-by-site-kind cross product in a stable
// order, annotation-major. This is the set of cells a format must either
// cover or explicitly excuse.
func Cells() []Cell {
	annotations, kinds := Annotations(), SiteKinds()
	out := make([]Cell, 0, len(annotations)*len(kinds))
	for _, a := range annotations {
		for _, k := range kinds {
			out = append(out, Cell{Annotation: a, SiteKind: k})
		}
	}
	return out
}

// MissingCells returns the cells that covered does not include, in Cells
// order. Entries in covered that are not real cells are ignored, so a caller
// cannot accidentally satisfy annotation retention with a typo.
func MissingCells(covered []Cell) []Cell {
	have := make(map[Cell]bool, len(covered))
	for _, c := range covered {
		have[c] = true
	}
	var out []Cell
	for _, c := range Cells() {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}
