package typed

import (
	"context"
	"errors"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestSchema_ReportsADecoderFailureAndRemembersNothing pins the one failure
// the parser's decoder can return for a node a position holds: it is reported
// with the position, and nothing is kept for a later ask.
func TestSchema_ReportsADecoderFailureAndRemembersNothing(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-lib: {S: {}}\n"))
	require.NoError(t, err)
	b := New(doc, doc.GetRootNode())
	b.decode = func(context.Context, *yaml.Node, *schemaRef) error { return errors.New("planted") }

	_, err = b.Schema(t.Context(), "/x-lib/S", bind.Raw)
	require.Error(t, err)
	assert.Equal(t, "building the schema at /x-lib/S: planted", err.Error())
	assert.Empty(t, b.built)
}
