package harness

// Annotation names one annotation slot the IR offers at a site. It is test
// vocabulary: nothing in the compiler's write path refers to these values,
// and ir.Diagnostic carries no annotation field. Annotation is one axis of
// annotation retention; SiteKind is the other.
//
// JSON Schema Core uses "annotation" more narrowly than this suite does:
// minimum/maxLength are assertions in its terms, and if/then/else, not,
// dependentSchemas are applicators. This suite groups them under Annotation
// anyway because, from the IR's perspective, each attaches to a source
// position rather than defines the type there — the distinction retention
// actually measures.
type Annotation string

// The annotation slots the IR offers. Adding a slot here widens what
// annotation retention checks, which is the intended way to discover that a
// format handles it nowhere — TestConstBlock_TiesToAnnotationsAndSiteKinds
// (annotations_test.go) requires every constant declared below to also
// appear in Annotations(), so a slot added here without also being added
// there fails that test rather than sitting unused.
const (
	AnnotationDocs            Annotation = "docs"
	AnnotationExamples        Annotation = "examples"
	AnnotationConstraints     Annotation = "constraints"
	AnnotationDefault         Annotation = "default"
	AnnotationDeprecated      Annotation = "deprecated"
	AnnotationVisibility      Annotation = "visibility"
	AnnotationVendorExtension Annotation = "vendorExtension"
	AnnotationXMLHints        Annotation = "xmlHints"
	AnnotationValidationOnly  Annotation = "validationOnly"
)

// SiteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own, and both from
// a carrier — a position whose annotations land on an ir.Property or
// ir.Parameter instead of on a type node at all.
//
// Declaration further splits by the declaring component's own shape, because the
// compiler routes an object-shaped body and a scalar-shaped body through
// different lowering paths that do not honor the same keywords: an
// annotation present on one shape is not evidence it survives on the other.
//
// SiteDeclarationModel and SiteDeclarationScalar do not cover every
// declaration shape the OpenAPI compiler's schema dispatch (lower(), in
// schema.go) produces — const (hoistLiteral -> Literal), enum (lowerEnum ->
// Enum or a Union of Literals), a multi-type such as `type: [string,
// integer]` (lowerUnion -> Union), a `type: array` body (lowerArray ->
// List/Tuple), allOf (lowerAllOf -> Model), a sibling-less oneOf/anyOf
// (lowerOneOfAnyOf -> Union), and a oneOf/anyOf co-declared with a structural
// sibling (lowerWithUnionSiblings -> lowerModel) are destinations too. None of
// them reads annotations: attachDeclaredAnnotations runs above the dispatch,
// so retention is a property of the reading's position rather than of the
// destination reached (GitHub #114).
//
// That claim is checked rather than assumed, but not at every destination.
// TestAnnotationRetention_OtherDeclarationShapes (annotations_test.go) runs
// the full nine-annotation grid at four of them — const, enum, array, and a
// sibling-less oneOf — and pins the node kind each lowers to, so the four
// rows stay four distinct destinations. allOf, a multi-type, and a
// structurally co-declared oneOf/anyOf are still unexercised: each lands on a
// Model or Union node the checked rows already cover, so nothing about them
// is expected to differ, but nothing here proves it.
type SiteKind string

// The kinds of position an annotation can be written at.
// TestConstBlock_TiesToAnnotationsAndSiteKinds requires every constant below
// to appear in SiteKinds(), the same way it does for Annotations() above.
const (
	// SiteDeclarationModel is an annotation on an object-shaped component,
	// which lowers through the compiler's model path (lowerModel).
	SiteDeclarationModel SiteKind = "declaration-model"
	// SiteDeclarationScalar is an annotation on a scalar-shaped component,
	// which lowers through the compiler's alias path (internAlias in
	// schema.go).
	SiteDeclarationScalar SiteKind = "declaration-scalar"
	// SiteReference is an annotation written beside a $ref.
	SiteReference SiteKind = "reference"
	// SiteCarrierProperty is an annotation on a model property's schema, which
	// the compiler copies onto the ir.Property rather than onto a type node.
	SiteCarrierProperty SiteKind = "carrier-property"
	// SiteCarrierParameter is an annotation on an operation parameter's schema,
	// which the compiler copies onto the ir.Parameter.
	//
	// It is a separate kind from SiteCarrierProperty rather than one "carrier"
	// row covering both, because the two carriers hold different sets: ir.Property
	// has XML and Visibility fields and ir.Parameter has neither, so an annotation
	// surviving at one is no evidence at all that it survives at the other. Both
	// defects that reached a parameter position were of exactly that shape.
	SiteCarrierParameter SiteKind = "carrier-parameter"
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
		AnnotationVendorExtension,
		AnnotationXMLHints,
		AnnotationValidationOnly,
	}
}

// SiteKinds returns every site kind in a stable order.
func SiteKinds() []SiteKind {
	return []SiteKind{
		SiteDeclarationModel, SiteDeclarationScalar, SiteReference,
		SiteCarrierProperty, SiteCarrierParameter,
	}
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
