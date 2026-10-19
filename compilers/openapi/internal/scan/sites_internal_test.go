package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// bothOrders is a document declaring members under header as given and
// reversed, each member one line.
func bothOrders(header string, members []string) map[string][]byte {
	reversed := slices.Clone(members)
	slices.Reverse(reversed)
	return map[string][]byte{
		"as written": []byte(header + strings.Join(members, "")),
		"reversed":   []byte(header + strings.Join(reversed, "")),
	}
}

// onlyRefusal runs the scan over data and returns its one refusal, which it
// requires.
func onlyRefusal(t *testing.T, data []byte) ir.Diagnostic {
	t.Helper()
	diags := scanPlaced(t, data)
	require.Len(t, diags, 1, "in\n%s", data)
	require.Equal(t, ir.SeverityError, diags[0].Severity)
	return diags[0]
}

// TestRefusal_NamesTheCycleNotWhatLeadsIntoIt is GitHub #912. A reference
// that only leads into a cycle is not named, and of a cycle's members, or of
// several cycles', the one with the least pointer is, whichever is declared
// first.
func TestRefusal_NamesTheCycleNotWhatLeadsIntoIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		want    jsontext.Pointer
	}{
		{"a reference into a self-loop", []string{
			"    R: {$ref: '#/components/schemas/A'}\n",
			"    A: {$ref: '#/components/schemas/A'}\n",
		}, "/components/schemas/A"},
		{"two self-loops", []string{
			"    Z: {$ref: '#/components/schemas/Z'}\n",
			"    Y: {$ref: '#/components/schemas/Y'}\n",
		}, "/components/schemas/Y"},
		{"a loop of three", []string{
			"    C: {$ref: '#/components/schemas/A'}\n",
			"    B: {$ref: '#/components/schemas/C'}\n",
			"    A: {$ref: '#/components/schemas/B'}\n",
		}, "/components/schemas/A"},
	}
	for _, tc := range tests {
		for order, data := range bothOrders(schemasHeader, tc.members) {
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				d := onlyRefusal(t, data)
				assert.Equal(t, diag.CyclicRef, d.Code)
				assert.Equal(t, tc.want, d.Provenance.Pointer)
				assert.Equal(t, "cyclic $ref: reference chain never reaches a node without a $ref", d.Message)
			})
		}
	}
}

// TestCycles_NamesTheLeastRecursiveAlias pins the site of a recursive anchor
// when a document holds several: the alias with the least pointer, in either
// order. Each anchor stays above its own alias, so both orders parse.
func TestCycles_NamesTheLeastRecursiveAlias(t *testing.T) {
	t.Parallel()
	members := []string{"b: &x {k: [*x]}\n", "a: &y {k: [*y]}\n"}
	for order, data := range bothOrders("", members) {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			d := onlyRefusal(t, data)
			assert.Equal(t, diag.CyclicRef, d.Code)
			assert.Equal(t, jsontext.Pointer("/a/k/0"), d.Provenance.Pointer)
			assert.Equal(t, `recursive YAML anchor "y" references an ancestor node`, d.Message)
		})
	}
}

// laughs is a billion-laughs bomb named prefix: eight anchors, each a list of
// ten aliases to the one before.
func laughs(prefix string) string {
	const levels = 8
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n  l0: &%s0 [x, x, x, x, x, x, x, x, x, x]\n", prefix, prefix)
	for i := 1; i < levels; i++ {
		aliases := strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*%s%d, ", prefix, i-1), 10), ", ")
		fmt.Fprintf(&b, "  l%d: &%s%d [%s]\n", i, prefix, i, aliases)
	}
	return b.String()
}

// TestAliasAmplification_NamesTheLeastInnermostAmplifier pins the site of an
// alias bomb: the smallest structure already past the allowance, l4 of ten
// levels each ten times the last, not the bomb or the level holding it; and of
// two bombs, the one with the least pointer, in either order.
func TestAliasAmplification_NamesTheLeastInnermostAmplifier(t *testing.T) {
	t.Parallel()
	for order, data := range bothOrders("", []string{laughs("b"), laughs("a")}) {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			d := onlyRefusal(t, data)
			assert.Equal(t, diag.AliasAmplification, d.Code)
			assert.Equal(t, jsontext.Pointer("/a/l4"), d.Provenance.Pointer)
		})
	}
}

// TestLoopsReached_GathersALoopOnce pins that a loop is gathered once however
// many refused chains run into it: every member of a loop of n is a start, so
// a gathering per start would hold n² sites.
func TestLoopsReached_GathersALoopOnce(t *testing.T) {
	t.Parallel()
	const n = 300
	s, chains := settled(t, schemasDoc(slices.Concat(chainTo("S", n, "C0"), loopOf(n))))

	assert.Len(t, chains.loopsReached(s.out), n)
}

// fanned is an anchor of ten scalars under name, aliased twenty times in a
// list beside it.
func fanned(name string) string {
	return fmt.Sprintf("%s: {x: &%s [1, 1, 1, 1, 1, 1, 1, 1, 1, 1], u: [%s]}\n", name, name,
		strings.TrimSuffix(strings.Repeat("*"+name+", ", 20), ", "))
}

// TestAliasAmplification_NamesTheLeastSiteUnderABudget pins the two budgeted
// branches, in either order: two lists each adding more than the budget allows
// but staying inside the ratio, named for the budget; and, under the same
// budget, two bombs past the ratio, named for their shape.
func TestAliasAmplification_NamesTheLeastSiteUnderABudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		code    string
		want    jsontext.Pointer
	}{
		{"past the budget", []string{fanned("b"), fanned("a")}, diag.BudgetExceeded, "/a/u"},
		{"past the ratio", []string{laughs("b"), laughs("a")}, diag.AliasAmplification, "/a/l4"},
	}
	for _, tc := range tests {
		for order, data := range bothOrders("", tc.members) {
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				root := indexOf(t, data).Root()
				d, refused := aliasAmplification(InTree(0, Pointers(root)), root, rawNodes(root), 100)
				require.True(t, refused)
				assert.Equal(t, tc.code, d.Code)
				assert.Equal(t, tc.want, d.Provenance.Pointer)
			})
		}
	}
}
