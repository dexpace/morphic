package resolve_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// TestUseDocs_UseSiteSiblingsOverrideTheDeclaration pins the fold at one hop: a
// summary or description written beside a $ref wins field by field over the
// declaration's, and one the entry omits leaves the declaration's standing. A
// hand-built Reference resolves nothing, so it is a chain of one; the parser's
// longer chains are pinned where a document builds them.
func TestUseDocs_UseSiteSiblingsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	summary, description := "use-site summary", "use-site description"
	declared := ir.Docs{Summary: "declared summary", Description: "declared description"}

	for _, tc := range []struct {
		name string
		ref  *soa.ReferencedParameter
		want ir.Docs
	}{
		{
			name: "both siblings win",
			ref:  &soa.ReferencedParameter{Summary: &summary, Description: &description},
			want: ir.Docs{Summary: "use-site summary", Description: "use-site description"},
		},
		{
			// The parser fills these two only for an entry that really is a $ref
			// (Reference.Populate), so an inline entry arrives with both nil.
			name: "an inline entry writes neither",
			ref:  &soa.ReferencedParameter{},
			want: declared,
		},
		{
			name: "a description alone leaves the declaration's summary standing",
			ref:  &soa.ReferencedParameter{Description: &description},
			want: ir.Docs{Summary: "declared summary", Description: "use-site description"},
		},
		{
			name: "a nil wrapper reports nothing",
			ref:  nil,
			want: declared,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tc.want, resolve.UseDocs[soa.Parameter](tc.ref, declared)))
		})
	}
}
