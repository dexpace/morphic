package archtest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grammarOwners are the packages permitted to spell the naming grammar
// themselves: ir, which declares the field and implements the grammar beside it,
// and the framework, whose NamingFor pairs a source spelling with the words
// derived from it.
var grammarOwners = []string{"compilers/compile", "ir"}

// nameChannels are the ir.Naming fields that carry a name for an emitter to
// render, and so the fields the grammar has to have produced. Both are held for
// the same reason and by the same rule: Canonical is the words of a declared
// name, Hint the words of a generated one, and an emitter reading either cannot
// tell which compiler wrote it (GitHub #163, #54).
//
// Source and Aliases are not here. They are source spellings kept as written,
// which is the opposite requirement.
var nameChannels = []string{"Canonical", "Hint"}

// TestNamingGrammar_NameChannelsAreFilledByTheFrameworkOnly asserts that no
// production package outside grammarOwners fills a nameChannels field itself.
//
// Those fields are ABI, so a compiler's own segmentation would make one mean
// two things (GitHub #163). Only a rule outside the compilers stops another
// copy being written.
//
// Not checked: a name filled from a local variable reads as a violation even if
// it came from the framework, so the call belongs at the site; and a literal
// inside package ir is spelled Naming, not ir.Naming, which is why ir is an
// owner rather than swept.
func TestNamingGrammar_NameChannelsAreFilledByTheFrameworkOnly(t *testing.T) {
	t.Parallel()
	offenders := sweepProduction(t, repoRoot(t), "", grammarOwners, nameChannelViolations)
	assert.Empty(t, offenders,
		"only %v may derive a name; everything else goes through compile.NamingFor, compile.NamingHint or ir.CanonicalWords",
		grammarOwners)
}

// TestNameChannelViolations_LocalGrammarIsCaught plants what the sweep exists to
// find — a compiler deriving its own name, in every shape it can be written, in
// both channels — and pins that the framework calls beside it stay clean.
// Without the planted half, a matcher recognizing nothing at all would pass the
// sweep above and read as proof.
func TestNameChannelViolations_LocalGrammarIsCaught(t *testing.T) {
	t.Parallel()
	const src = `package graphql

func lower(name string) []ir.Naming {
	declared := ir.Naming{Source: name, Canonical: canonicalWords(name)}
	framework := ir.Naming{Source: name, Canonical: ir.CanonicalWords(name)}
	hinted := ir.Naming{Hint: localWords(name)}
	fromFramework := ir.Naming{Hint: compile.SubHint(name, "item")}
	kept := ir.Naming{Source: name, Aliases: []string{name}}
	var late ir.Naming
	late.Canonical = strings.ToLower(name)
	late.Hint = strings.ToLower(name)
	return []ir.Naming{declared, framework, hinted, fromFramework, kept, late, {Canonical: lower(name)}}
}
`
	offenders, err := nameChannelViolations("planted.go", "compilers/graphql/naming.go", src)
	require.NoError(t, err)
	require.Len(t, offenders, 5,
		"the two literals, the two assignments and the elided literal — not the framework calls, and not Aliases: %v",
		offenders)
	assert.Contains(t, offenders[0], "Canonical is filled by canonicalWords(name)")
	assert.Contains(t, offenders[1], "Hint is filled by localWords(name)")
	assert.Contains(t, offenders[2], "Canonical is filled by strings.ToLower(name)")
	assert.Contains(t, offenders[3], "Hint is filled by strings.ToLower(name)")
	assert.Contains(t, offenders[4], "lower(name)", "an element of a []ir.Naming elides the type")
}

// idOwners is the package permitted to construct an ir ID type from a string.
var idOwners = []string{"compilers/compile"}

// idTypes are ir's ID types. A compiler converting a string into one of them is
// deriving an identifier.
var idTypes = []string{"TypeID", "OpID", "PropID", "AuthID", "ServiceID", "GroupID", "ParamID", "EnumMemberID", "ChannelID", "MessageID"}

// TestIDGrammar_CompilersDeriveIDsThroughTheFramework asserts that no compiler
// but the framework builds an ir ID out of a string, or a namespace out of
// anything but a literal.
//
// The derivation stays with the compiler (a JSON Pointer, a GraphQL structural
// path and a protobuf name are different things), but the grammar around it
// does not: the kind prefix, the namespace, and the rule that a minted node
// takes a namespace of its own (GitHub #162).
//
// The sweep covers the compilers, not the repository: pass and irverify convert
// an existing ID string back into its type to look a node up, which derives
// nothing.
func TestIDGrammar_CompilersDeriveIDsThroughTheFramework(t *testing.T) {
	t.Parallel()
	offenders := sweepProduction(t, repoRoot(t), "compilers", idOwners, idDerivations)
	assert.Empty(t, offenders,
		"only %v may build an ID from a string; a compiler supplies the path and the namespace", idOwners)
}

// TestIDDerivations_LocalGrammarIsCaught plants a compiler spelling an ID prefix
// itself — the shape every compiler used before the promotion — and pins that
// deriving through the framework beside it stays clean.
func TestIDDerivations_LocalGrammarIsCaught(t *testing.T) {
	t.Parallel()
	const src = `package graphql

const anyTypeID ir.TypeID = "t/graphql/any"

func ids(pointer string, existing ir.OpID) (ir.TypeID, ir.OpID, ir.PropID, ir.ParamID, ir.EnumMemberID) {
	named := ir.TypeID("t/graphql" + pointer)
	op := compile.OpID(graphqlSpace, pointer)
	var copied ir.OpID = existing
	prop := ir.PropID("p/graphql" + pointer)
	param := ir.ParamID("param/graphql" + pointer)
	member := ir.EnumMemberID("e/graphql" + pointer)
	return named, op, prop, param, member
}
`
	offenders, err := idDerivations("planted.go", "compilers/graphql/ids.go", src)
	require.NoError(t, err)
	require.Len(t, offenders, 5,
		"the constant and the four conversions, not the framework call or the copy: %v", offenders)
	assert.Contains(t, offenders[0], "declares a literal as an ir.TypeID")
	assert.Contains(t, offenders[1], "converts a string to an ir.TypeID")
	assert.Contains(t, offenders[2], "converts a string to an ir.PropID")
	assert.Contains(t, offenders[3], "converts a string to an ir.ParamID")
	assert.Contains(t, offenders[4], "converts a string to an ir.EnumMemberID")
}

// TestIDDerivations_SpaceBuiltFromDataIsCaught plants a compiler assembling a
// namespace from data, the shape that loses the separator before the path. The
// declaration, the glued conversion, the conversion of a variable and the
// conversion of a number are each reported. Literal namespaces, however used,
// and a declaration with no value are clean.
func TestIDDerivations_SpaceBuiltFromDataIsCaught(t *testing.T) {
	t.Parallel()
	const src = `package graphql

const fixedSpace compile.Space = "graphql"

var (
	glued   compile.Space = prefix + name
	pending compile.Space
)

func ids(name string) (ir.OpID, ir.OpID, ir.OpID, compile.Space, compile.Space) {
	fixed := compile.OpID(fixedSpace, "/"+name)
	literal := compile.OpID(compile.Space("graphql"), name)
	lost := compile.OpID(compile.Space("graphql"+name), "")
	return fixed, literal, lost, compile.Space(name), compile.Space(65)
}
`
	offenders, err := idDerivations("planted.go", "compilers/graphql/ids.go", src)
	require.NoError(t, err)
	require.Len(t, offenders, 4,
		"the declaration and three conversions that are not string literals; not the literals: %v", offenders)
	assert.Contains(t, offenders[0], "declares a compile.Space from a non-literal")
	for _, conversion := range offenders[1:] {
		assert.Contains(t, conversion, "converts a non-literal to a compile.Space")
	}
}

// TestIDDerivations_ImportsAreResolvedFromTheFile plants the two ways a name
// can mislead a rule that assumes the conventional one. Aliasing ir and compile
// must not hide a violation; a package that is merely called compile, which this
// repository's rule is not about, must not be reported for it.
func TestIDDerivations_ImportsAreResolvedFromTheFile(t *testing.T) {
	t.Parallel()
	const aliased = `package graphql

import (
	cmp "github.com/dexpace/morphic/compilers/compile"
	irx "github.com/dexpace/morphic/ir"
)

func ids(name string) (irx.OpID, cmp.Space) {
	return irx.OpID(name), cmp.Space(name)
}
`
	offenders, err := idDerivations("planted.go", "compilers/graphql/ids.go", aliased)
	require.NoError(t, err)
	require.Len(t, offenders, 2, "the conversion to an ID type and to a namespace, both through aliases: %v", offenders)
	assert.Contains(t, offenders[0], "converts a string to an ir.OpID")
	assert.Contains(t, offenders[1], "converts a non-literal to a compile.Space")

	const unrelated = `package graphql

import (
	"example.com/other/compile"
	"example.com/other/ir"
)

func ids(name string) (ir.OpID, compile.Space) {
	return ir.OpID(name), compile.Space(name)
}
`
	offenders, err = idDerivations("planted.go", "compilers/graphql/ids.go", unrelated)
	require.NoError(t, err)
	assert.Empty(t, offenders, "packages that are not ours are not the rule's business")
}

// idDerivations reports every place in one file that builds an ir ID out of a
// string: a conversion, and a typed declaration holding a literal — the shape
// that spells an ID without converting anything. src is nil to read the file at
// path, or the source itself.
func idDerivations(path, rel string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("archtest: parse %s: %w", path, err)
	}

	var found []string
	names := namesIn(file)
	report := func(pos token.Pos, how, idType string) {
		found = append(found, fmt.Sprintf("%s:%d: %s an ir.%s rather than deriving it through the framework",
			rel, fset.Position(pos).Line, how, idType))
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if idType, ok := idTypeName(node.Fun, names.ir); ok {
				report(node.Pos(), "converts a string to", idType)
			}
		case *ast.ValueSpec:
			// `const anyTypeID ir.TypeID = "t/protobuf/any"` — the Protobuf draft's
			// shape, which spells a whole ID and converts nothing. A declaration
			// with no literal in it is copying an ID, not deriving one.
			if idType, ok := idTypeName(node.Type, names.ir); ok && holdsStringLiteral(node.Values) {
				report(node.Pos(), "declares a literal as", idType)
			}
		default:
		}
		if how, bad := spaceFromData(n, names.compile); bad {
			found = append(found, fmt.Sprintf("%s:%d: %s; a compile.Space is a literal, or the path glued onto it loses its separator",
				rel, fset.Position(n.Pos()).Line, how))
		}
		return true
	})
	return found, nil
}

// spaceFromData reports a compile.Space built from anything but a string
// literal: a conversion of some other expression, or a declaration of the type
// whose value is one.
//
// The framework supplies the separator between a namespace and its path only
// when they arrive as separate arguments. A namespace assembled from data can
// carry the path in with it, which is how "t/anonaddr" lost its separator
// (GitHub #141). The rule is syntactic: two constants glued together slip past
// it, and Document.IDSpaces reports that as an undeclared namespace.
func spaceFromData(n ast.Node, compileName string) (how string, bad bool) {
	switch node := n.(type) {
	case *ast.CallExpr:
		if isSelector(node.Fun, compileName, "Space") && (len(node.Args) != 1 || !isStringLiteral(node.Args[0])) {
			return "converts a non-literal to a compile.Space", true
		}
	case *ast.ValueSpec:
		if isSelector(node.Type, compileName, "Space") && !allStringLiterals(node.Values) {
			return "declares a compile.Space from a non-literal", true
		}
	default:
	}
	return "", false
}

// isStringLiteral reports whether e is a string literal.
func isStringLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// allStringLiterals reports whether every expression is a string literal. No
// expressions at all is vacuously true: a declaration with no value builds
// nothing.
func allStringLiterals(exprs []ast.Expr) bool {
	for _, e := range exprs {
		if !isStringLiteral(e) {
			return false
		}
	}
	return true
}

// idTypeName returns the name of the ir ID type expr names, if it names one
// through irName, the identifier the file imports ir as.
func idTypeName(expr ast.Expr, irName string) (string, bool) {
	for _, idType := range idTypes {
		if isSelector(expr, irName, idType) {
			return idType, true
		}
	}
	return "", false
}

// derivationNames are the identifiers a file uses for the two packages the rule
// is about, or "" for one it does not import.
type derivationNames struct{ ir, compile string }

// namesIn resolves them from the file's own imports, so an aliased import cannot
// hide a violation and an unrelated package that happens to be called compile
// cannot cause one. A file with no imports at all is a snippet, read with the
// conventional names.
func namesIn(file *ast.File) derivationNames {
	return derivationNames{
		ir:      importName(file, module+"/ir", "ir"),
		compile: importName(file, module+"/compilers/compile", "compile"),
	}
}

// importName returns the identifier file refers to the package at path by, which
// is conventional unless the import renames it. A blank or dot import cannot be
// referred to by selector, and a package the file does not import cannot be
// referred to at all.
func importName(file *ast.File, path, conventional string) string {
	if len(file.Imports) == 0 {
		return conventional
	}
	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, "\"`") != path {
			continue
		}
		switch {
		case spec.Name == nil:
			return conventional
		case spec.Name.Name == "_" || spec.Name.Name == ".":
			return ""
		default:
			return spec.Name.Name
		}
	}
	return ""
}

// holdsStringLiteral reports whether any of exprs contains a string literal, so
// a declaration built from one is told apart from one assigned an existing ID.
func holdsStringLiteral(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		var seen bool
		ast.Inspect(expr, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				seen = true
			}
			return !seen
		})
		if seen {
			return true
		}
	}
	return false
}

// nameChannelViolations reports every place in one file that fills a
// nameChannels field of ir.Naming from something other than a call into the
// framework package, whether as a composite-literal field or an assignment
// afterwards.
//
// src is nil to read the file at path, or the source itself for a planted test;
// rel names the file in the messages.
func nameChannelViolations(path, rel string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("archtest: parse %s: %w", path, err)
	}

	var found []string
	report := func(field string, expr ast.Expr) {
		found = append(found, fmt.Sprintf("%s:%d: %s is filled by %s rather than by the framework",
			rel, fset.Position(expr.Pos()).Line, field, exprText(fset, expr)))
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			reportNameChannelField(node, report)
		case *ast.AssignStmt:
			reportNameChannelAssign(node, report)
		default:
		}
		return true
	})
	return found, nil
}

// reportNameChannelField reports a name-channel field of a composite literal
// when it is not filled by a framework call.
//
// The literal's type is not required to be ir.Naming, and not only because an
// element of a []ir.Naming elides it: ir.Naming is the one type in the repository
// with a Canonical field, and the one with a Hint field — the neighbouring hint
// carriers are spelled MediaTypeHint and XMLHints — so the field name identifies
// it. A second type carrying either name would need this narrowed, and would be
// worth a look on its own.
func reportNameChannelField(lit *ast.CompositeLit, report func(string, ast.Expr)) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && slices.Contains(nameChannels, key.Name) && !isFrameworkCall(kv.Value) {
			report(key.Name, kv.Value)
		}
	}
}

// reportNameChannelAssign reports an assignment to a name-channel field whose
// value is not a framework call. It closes the way round the literal check: build
// an empty Naming, then fill the field.
func reportNameChannelAssign(stmt *ast.AssignStmt, report func(string, ast.Expr)) {
	for i, lhs := range stmt.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || !slices.Contains(nameChannels, sel.Sel.Name) {
			continue
		}
		// A multi-value right-hand side has no single expression to attribute to
		// the field, and no legitimate shape assigns a name channel that way.
		if len(stmt.Rhs) != len(stmt.Lhs) {
			report(sel.Sel.Name, lhs)
			continue
		}
		if !isFrameworkCall(stmt.Rhs[i]) {
			report(sel.Sel.Name, stmt.Rhs[i])
		}
	}
}

// isFrameworkCall reports whether expr is a call on a package that owns the
// grammar — compile.NamingFor(name), compile.SubHint(...) or
// ir.CanonicalWords(name). Any of their
// functions counts: the rule is where the grammar lives, not which entry point
// reaches it, and the two entry points sit in different packages because the
// grammar is beside the field it fills while the constructor that pairs it with
// a source spelling is compiler-facing.
func isFrameworkCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && (pkg.Name == "compile" || pkg.Name == "ir")
}

// exprText renders expr as source, so a failure names the expression a reader has
// to go and change rather than only where it is.
func exprText(fset *token.FileSet, expr ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, expr); err != nil {
		return "<unprintable expression>"
	}
	return b.String()
}

// sweepProduction runs scan over every production Go file under the repo-relative
// subtree (empty for the whole repository), except those inside owners, and
// returns everything the scans found. Paths reported to scan stay repo-relative
// whatever the subtree, so a failure names a file the reader can open.
//
// It is shared by the rules about what a package may write rather than what it
// may import, so each new rule is a scan function rather than another tree walk.
func sweepProduction(t *testing.T, root, subtree string, owners []string,
	scan func(path, rel string, src any) ([]string, error),
) []string {
	t.Helper()
	base := filepath.Join(root, subtree)
	var found []string
	var scanned int
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipUninterestingDir(base, p, d)
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		require.NoError(t, relErr)
		slashed := filepath.ToSlash(rel)
		if hasPrefixDir(slashed, owners) {
			return nil
		}
		hits, scanErr := scan(p, slashed, nil)
		if scanErr != nil {
			return scanErr
		}
		scanned++
		found = append(found, hits...)
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, scanned, "the sweep reached no production Go file, so an empty result proves nothing")
	return found
}
