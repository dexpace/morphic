package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dexpace/morphic/internal/decls"
)

const (
	// fragmentPackage is the one package that reads a $ref fragment as a
	// pointer, and the only one that may use what bypasses its reading.
	fragmentPackage = module + "/compilers/openapi/internal/fragment"
	// referencesPackage declares the library's reference type, whose
	// GetJSONPointer query-unescapes a fragment and so reads '+' as a space.
	referencesPackage = "github.com/speakeasy-api/openapi/references"
)

// fragmentBypass is a use of what reads a $ref fragment otherwise than
// fragment.Pointer does.
type fragmentBypass struct {
	file string
	line int
	what string
}

func (b fragmentBypass) String() string {
	return fmt.Sprintf("%s:%d: %s; use fragment.Pointer", b.file, b.line, b.what)
}

// fragmentReading names what object is, when it is one of the two readings of a
// fragment that fragment.Pointer replaces, or "" for anything else: the method
// references.Reference.GetJSONPointer, and the function url.QueryUnescape it is
// built on. The library's CoreModel.GetJSONPointer is a different method and is
// not matched.
func fragmentReading(object types.Object) string {
	fn, ok := object.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	fn = fn.Origin()
	recv := fn.Signature().Recv()
	switch {
	case recv == nil && fn.Pkg().Path() == "net/url" && fn.Name() == "QueryUnescape":
		return "url.QueryUnescape"
	case recv != nil && fn.Name() == "GetJSONPointer" && receiverIs(recv.Type(), referencesPackage, "Reference"):
		return "references.Reference.GetJSONPointer"
	default:
		return ""
	}
}

// receiverIs reports whether t, through a pointer or an alias, is the named type
// name declared in the package at path.
func receiverIs(t types.Type, path, name string) bool {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == path && named.Obj().Name() == name
}

// fragmentBypasses returns each use, in the package at path, of a reading of a
// fragment that fragment.Pointer replaces, in file and line order. The package
// is loaded without its test files. A use is a call, a method value or a method
// expression: it reads what the checker resolved each identifier to, so a name
// that merely looks alike is not one. The fragment package itself is the one
// place they belong.
func fragmentBypasses(path string, info *types.Info, fset *token.FileSet) []fragmentBypass {
	if path == fragmentPackage {
		return nil
	}
	var found []fragmentBypass
	for id, object := range info.Uses {
		what := fragmentReading(object)
		if what == "" {
			continue
		}
		pos := fset.Position(id.Pos())
		found = append(found, fragmentBypass{file: pos.Filename, line: pos.Line, what: what})
	}
	slices.SortFunc(found, func(a, b fragmentBypass) int {
		if c := strings.Compare(a.file, b.file); c != 0 {
			return c
		}
		return a.line - b.line
	})
	return found
}

// TestFragmentReading_OnlyInPackageFragment holds the one reading of a $ref
// fragment (GitHub #911): outside compilers/openapi/internal/fragment, no
// production code calls references.Reference.GetJSONPointer or
// url.QueryUnescape. The resolver reads '+' in a fragment as a space where
// RFC 3986 reads a plus, so a reader of its own parts company with every other
// on the first '+', and derives an ID from a key the source does not spell.
func TestFragmentReading_OnlyInPackageFragment(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	require.Greater(t, len(pkgs), 1, "the sweep loaded no production package, so a clean result proves nothing")

	sawFragment := false
	for _, p := range pkgs {
		sawFragment = sawFragment || p.Types.Path() == fragmentPackage
		for _, b := range fragmentBypasses(p.Types.Path(), p.Info, p.Fset) {
			t.Error(b)
		}
	}
	assert.True(t, sawFragment, "the fragment package was not among those swept, so its exemption was never tried")
}

// plantedBypass uses both readings in every shape a use takes, beside two
// names that look alike and are not: a method of another type, and the
// function that decodes a path segment.
const plantedBypass = `package %s

import (
	"net/url"

	"github.com/speakeasy-api/openapi/references"
)

type lookalike struct{}

func (lookalike) GetJSONPointer() string { return "" }

func plantedBypass(r references.Reference) {
	_ = r.GetJSONPointer()
	read := r.GetJSONPointer
	_ = read
	_ = references.Reference.GetJSONPointer
	_, _ = url.QueryUnescape("a+b")
	_ = lookalike{}.GetJSONPointer()
	_, _ = url.PathUnescape("a+b")
}
`

// loadPlanted type-checks the package at dir, a directory of the repository,
// with plantedBypass added to it as a file of its own, and returns what
// fragmentBypasses finds there.
func loadPlanted(t *testing.T, dir, name string) []fragmentBypass {
	t.Helper()
	p := loadOverlaid(t, dir, "zz_planted.go", fmt.Sprintf(plantedBypass, name))
	return fragmentBypasses(p.Types.Path(), p.Info, p.Fset)
}

// loadOverlaid type-checks the package at dir, a directory of the repository,
// with src added to it as the file named file, and returns it.
func loadOverlaid(t *testing.T, dir, file, src string) *decls.Package {
	t.Helper()
	root := repoRoot(t)
	planted := filepath.Join(root, filepath.FromSlash(dir), file)
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:     root,
		Env:     append(os.Environ(), "GOPACKAGESDRIVER=off"),
		Overlay: map[string][]byte{planted: []byte(src)},
	}
	loaded, err := packages.Load(cfg, module+"/"+dir)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Empty(t, loaded[0].Errors, "the plant must type-check, or it proves nothing")
	hasPlant := slices.ContainsFunc(loaded[0].Syntax, func(f *ast.File) bool {
		return loaded[0].Fset.Position(f.Pos()).Filename == planted
	})
	require.True(t, hasPlant, "the plant was not part of the package")
	return &decls.Package{Types: loaded[0].Types, Info: loaded[0].TypesInfo, Syntax: loaded[0].Syntax, Fset: loaded[0].Fset}
}

// TestFragmentBypasses_PlantedUsesAreFound plants the uses the sweep above
// exists to find into a package that has none, in every shape a use takes, and
// requires exactly those. Without it a matcher that recognized nothing would
// pass the sweep and read as proof. The same plant in the fragment package is
// the exemption, and must find none.
func TestFragmentBypasses_PlantedUsesAreFound(t *testing.T) {
	t.Parallel()
	got := loadPlanted(t, "compilers/openapi/internal/diag", "diag")

	seen := make([]string, 0, len(got))
	for _, b := range got {
		assert.Equal(t, "zz_planted.go", filepath.Base(b.file))
		seen = append(seen, fmt.Sprintf("%d %s", b.line, b.what))
	}
	assert.Equal(t, []string{
		"14 references.Reference.GetJSONPointer",
		"15 references.Reference.GetJSONPointer",
		"17 references.Reference.GetJSONPointer",
		"18 url.QueryUnescape",
	}, seen, "a call, a method value, a method expression and the decoder, and neither look-alike")
	assert.Contains(t, got[0].String(), "use fragment.Pointer")
}

func TestFragmentBypasses_TheFragmentPackageMayUseThem(t *testing.T) {
	t.Parallel()
	assert.Empty(t, loadPlanted(t, "compilers/openapi/internal/fragment", "fragment"))
}
