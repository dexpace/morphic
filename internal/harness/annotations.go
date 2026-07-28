package harness

// Annotation names one annotation slot the IR offers at a site. It is test
// vocabulary: nothing in the compiler's write path refers to these values, and
// ir.Diagnostic carries no annotation field. Annotation is one axis of
// annotation retention; SiteKind is the other.
type Annotation string

// The annotation slots the IR offers. Adding a slot here widens what
// annotation retention checks, which is the intended way to discover that a
// format handles it nowhere.
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
type SiteKind string

// The kinds of position an annotation can be written at.
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
	Annotation Annotation `json:"annotation"`
	SiteKind   SiteKind   `json:"siteKind"`
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
