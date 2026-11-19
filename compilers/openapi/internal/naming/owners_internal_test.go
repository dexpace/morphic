// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// TestOwnerOf_NamesOnlyAMediaTypeAContentMapHolds pins the guards on a media
// type's owner. The grammar reads a media type only as a components/mediaTypes
// entry or an entry of a content map, whose holder is a request body, a
// response, a parameter or a header, so neither guard is reached through At;
// each stands for a grammar that grows a media type anywhere else, which must
// name it nothing rather than read past the pointer's start.
func TestOwnerOf_NamesOnlyAMediaTypeAContentMapHolds(t *testing.T) {
	t.Parallel()
	namer := New(nil, true)
	hint, ok := namer.ownerOf(reftable.Source, []string{"schema"}, []reftable.Kind{reftable.KindMediaType, reftable.KindSchema}, 0)
	assert.False(t, ok, "a media type no content map holds")
	assert.Empty(t, hint)

	hint, ok = namer.contentOwner(reftable.Source, "/x", reftable.KindOther)
	assert.False(t, ok, "content held by no object the grammar names")
	assert.Empty(t, hint)
}
