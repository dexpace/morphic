package harness

// Aspect names one annotation slot the IR offers at a site. It is test
// vocabulary: nothing in the compiler's write path refers to these values, and
// ir.Diagnostic carries no aspect field. Aspects form one axis of the
// conformance grid.
type Aspect string

// The annotation slots the IR offers. Adding a slot here widens the grid, which
// is the intended way to discover that a format handles it nowhere.
const (
	AspectDocs           Aspect = "docs"
	AspectExamples       Aspect = "examples"
	AspectConstraints    Aspect = "constraints"
	AspectDefault        Aspect = "default"
	AspectDeprecated     Aspect = "deprecated"
	AspectVisibility     Aspect = "visibility"
	AspectExtensions     Aspect = "extensions"
	AspectXMLHints       Aspect = "xmlHints"
	AspectValidationOnly Aspect = "validationOnly"
)

// SiteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own. Declaration
// splits by the declaring component's own shape because the compiler routes an
// object-shaped body and a scalar-shaped body through different lowering paths
// that do not honor the same keywords: an annotation present on one shape is
// not evidence it survives on the other. The distinction is the grid's second
// axis because this is where annotations are most often dropped.
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

// Cell identifies one square of the conformance grid: one annotation slot at
// one kind of position.
type Cell struct {
	Aspect   Aspect   `json:"aspect"`
	SiteKind SiteKind `json:"siteKind"`
}

// Aspects returns every aspect in a stable order.
func Aspects() []Aspect {
	return []Aspect{
		AspectDocs,
		AspectExamples,
		AspectConstraints,
		AspectDefault,
		AspectDeprecated,
		AspectVisibility,
		AspectExtensions,
		AspectXMLHints,
		AspectValidationOnly,
	}
}

// SiteKinds returns every site kind in a stable order.
func SiteKinds() []SiteKind {
	return []SiteKind{SiteDeclarationModel, SiteDeclarationScalar, SiteReference}
}

// Cells returns the full aspect-by-site-kind cross product in a stable order,
// aspect-major. This is the grid a format must either cover or explicitly
// excuse.
func Cells() []Cell {
	aspects, kinds := Aspects(), SiteKinds()
	out := make([]Cell, 0, len(aspects)*len(kinds))
	for _, a := range aspects {
		for _, k := range kinds {
			out = append(out, Cell{Aspect: a, SiteKind: k})
		}
	}
	return out
}

// MissingCells returns the grid cells that covered does not include, in Cells
// order. Entries in covered that are not grid cells are ignored, so a caller
// cannot accidentally satisfy the grid with a typo.
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
