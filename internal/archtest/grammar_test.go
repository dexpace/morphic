package archtest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
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

// TestNamingGrammar_CanonicalIsFilledByTheFrameworkOnly asserts that no
// production package outside grammarOwners fills ir.Naming.Canonical itself.
//
// Canonical is ABI: an emitter reading it cannot tell which compiler produced the
// name, so a compiler holding its own segmentation opinion makes the field mean
// two things at once. That is not hypothetical — three copies of the grammar
// disagreed about "." and about every other non-word character (GitHub #163) —
// and deleting two copies does not stop a fourth being written. Only a rule
// outside the compilers does.
//
// Deliberately not checked: a Canonical filled from a local variable reads as a
// violation even when the variable came from the framework, so the call belongs
// at the site; Naming.Hint is out of scope and is cased today (GitHub #54); and a
// literal inside package ir is spelled Naming rather than ir.Naming, which is one
// reason ir is an owner rather than a swept package.
func TestNamingGrammar_CanonicalIsFilledByTheFrameworkOnly(t *testing.T) {
	t.Parallel()
	offenders := sweepProduction(t, repoRoot(t), "", grammarOwners, canonicalViolations)
	assert.Empty(t, offenders,
		"only %v may derive a canonical name; everything else goes through compile.NamingFor or ir.CanonicalWords",
		grammarOwners)
}

// TestCanonicalViolations_LocalGrammarIsCaught plants what the sweep exists to
// find — a compiler deriving its own canonical, in both the shapes it can be
// written — and pins that the framework call beside it stays clean. Without the
// planted half, a matcher recognizing nothing at all would pass the sweep above
// and read as proof.
func TestCanonicalViolations_LocalGrammarIsCaught(t *testing.T) {
	t.Parallel()
	const src = `package graphql

func lower(name string) []ir.Naming {
	declared := ir.Naming{Source: name, Canonical: canonicalWords(name)}
	framework := ir.Naming{Source: name, Canonical: ir.CanonicalWords(name)}
	hint := ir.Naming{Hint: localWords(name)}
	var late ir.Naming
	late.Canonical = strings.ToLower(name)
	return []ir.Naming{declared, framework, hint, late, {Canonical: lower(name)}}
}
`
	offenders, err := canonicalViolations("planted.go", "compilers/graphql/naming.go", src)
	require.NoError(t, err)
	require.Len(t, offenders, 3,
		"the literal, the assignment and the elided literal — not the framework call or the hint: %v", offenders)
	assert.Contains(t, offenders[0], "canonicalWords(name)")
	assert.Contains(t, offenders[1], "strings.ToLower(name)")
	assert.Contains(t, offenders[2], "lower(name)", "an element of a []ir.Naming elides the type")
}

// idOwners is the package permitted to construct an ir ID type from a string.
var idOwners = []string{"compilers/compile"}

// idTypes are ir's ID types. A compiler converting a string into one of them is
// deriving an identifier.
var idTypes = []string{"TypeID", "OpID", "PropID", "AuthID", "ServiceID", "ChannelID", "MessageID"}

// TestIDGrammar_CompilersDeriveIDsThroughTheFramework asserts that no compiler
// but the framework builds an ir ID out of a string.
//
// The derivation stays with the compiler — a JSON Pointer, a GraphQL structural
// path and a protobuf fully-qualified name are different things — but the grammar
// around it does not: the kind prefix, the namespace, and the rule that a minted
// node takes a namespace of its own (GitHub #162). Three compilers each spelled
// that themselves, and two of the three left the anonymous namespace unqualified
// by format, so nothing but coincidence kept their IDs apart.
//
// The sweep is the compilers rather than the repository because converting a
// string that is already an ID back into its type is legitimate elsewhere:
// pass and irverify do it to look a node up, which derives nothing.
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

func ids(pointer string, existing ir.OpID) (ir.TypeID, ir.OpID, ir.PropID) {
	named := ir.TypeID("t/graphql" + pointer)
	op := compile.OpID(graphqlSpace, pointer)
	var copied ir.OpID = existing
	prop := ir.PropID("p/graphql" + pointer)
	return named, op, prop
}
`
	offenders, err := idDerivations("planted.go", "compilers/graphql/ids.go", src)
	require.NoError(t, err)
	require.Len(t, offenders, 3,
		"the constant and the two conversions, not the framework call or the copy: %v", offenders)
	assert.Contains(t, offenders[0], "declares a literal as an ir.TypeID")
	assert.Contains(t, offenders[1], "converts a string to an ir.TypeID")
	assert.Contains(t, offenders[2], "converts a string to an ir.PropID")
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
	report := func(pos token.Pos, how, idType string) {
		found = append(found, fmt.Sprintf("%s:%d: %s an ir.%s rather than deriving it through the framework",
			rel, fset.Position(pos).Line, how, idType))
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if idType, ok := idTypeName(node.Fun); ok {
				report(node.Pos(), "converts a string to", idType)
			}
		case *ast.ValueSpec:
			// `const anyTypeID ir.TypeID = "t/protobuf/any"` — the Protobuf draft's
			// shape, which spells a whole ID and converts nothing. A declaration
			// with no literal in it is copying an ID, not deriving one.
			if idType, ok := idTypeName(node.Type); ok && holdsStringLiteral(node.Values) {
				report(node.Pos(), "declares a literal as", idType)
			}
		default:
		}
		return true
	})
	return found, nil
}

// idTypeName returns the name of the ir ID type expr names, if it names one.
func idTypeName(expr ast.Expr) (string, bool) {
	for _, idType := range idTypes {
		if isSelector(expr, "ir", idType) {
			return idType, true
		}
	}
	return "", false
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

// canonicalViolations reports every place in one file that fills
// ir.Naming.Canonical from something other than a call into the framework
// package, whether as a composite-literal field or an assignment afterwards.
//
// src is nil to read the file at path, or the source itself for a planted test;
// rel names the file in the messages.
func canonicalViolations(path, rel string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("archtest: parse %s: %w", path, err)
	}

	var found []string
	report := func(expr ast.Expr) {
		found = append(found, fmt.Sprintf("%s:%d: Canonical is filled by %s rather than by the framework",
			rel, fset.Position(expr.Pos()).Line, exprText(fset, expr)))
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			reportCanonicalField(node, report)
		case *ast.AssignStmt:
			reportCanonicalAssign(node, report)
		default:
		}
		return true
	})
	return found, nil
}

// reportCanonicalField reports a Canonical field of a composite literal when it
// is not filled by a framework call.
//
// The literal's type is not required to be ir.Naming, and not only because an
// element of a []ir.Naming elides it: ir.Naming is the one type in the repository
// with a Canonical field, so the field name identifies it. A second type carrying
// that name would need this narrowed — and would be worth a look on its own.
func reportCanonicalField(lit *ast.CompositeLit, report func(ast.Expr)) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && key.Name == "Canonical" && !isFrameworkCall(kv.Value) {
			report(kv.Value)
		}
	}
}

// reportCanonicalAssign reports an assignment to a .Canonical field whose value
// is not a framework call. It closes the way round the literal check: build an
// empty Naming, then fill the field.
func reportCanonicalAssign(stmt *ast.AssignStmt, report func(ast.Expr)) {
	for i, lhs := range stmt.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Canonical" {
			continue
		}
		// A multi-value right-hand side has no single expression to attribute to
		// the field, and no legitimate shape assigns Canonical that way.
		if len(stmt.Rhs) != len(stmt.Lhs) {
			report(lhs)
			continue
		}
		if !isFrameworkCall(stmt.Rhs[i]) {
			report(stmt.Rhs[i])
		}
	}
}

// isFrameworkCall reports whether expr is a call on a package that owns the
// grammar — compile.NamingFor(name) or ir.CanonicalWords(name). Any of their
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
