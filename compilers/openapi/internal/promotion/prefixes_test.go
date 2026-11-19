// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// prefixedDoc keeps extensions under the scopes a document writes them in, on
// nodes of three kinds, for UnreadExtensions to find wherever they sit.
func prefixedDoc() *ir.Document {
	at := func(pointer string) ir.UnmodeledEntry {
		unmodeledEntry := vendorExtension(`"x"`)
		unmodeledEntry.Provenance.Pointer = jsontext.Pointer("/" + pointer)
		return unmodeledEntry
	}
	verbatim := at("allOf/0")
	verbatim.Reason = ir.ReasonNoIRHome
	return &ir.Document{
		Unmodeled: ir.Unmodeled{"openapi:tags/0/x-acme-stabilty": at("tags/0/x-acme-stabilty")},
		Types: ir.TypeRegistry{"type/s": &ir.Scalar{ID: "type/s", Unmodeled: ir.Unmodeled{
			"openapi:x-acme-stability": at("s/x-acme-stability"),
			"openapi:x-acme-sunset":    at("s/x-acme-sunset"),
			"openapi:x-other":          at("s/x-other"),
			"openapi:allOf/0":          verbatim,
		}}},
		Servers: map[ir.ServerID]ir.Server{"server/s": {ID: "server/s", Unmodeled: ir.Unmodeled{
			"openapi:content/a~1b/x-acme-s~1tability": at("server/x-acme-s~1tability"),
		}}},
	}
}

// TestUnreadExtensions_ReportsAnOwnedKeyNoSourceReads pins the owned-prefix
// check: an extension under a prefix the policy owns that no source reads is a
// warning naming the nearest key the mapping reads, wherever and under whatever
// scope a node keeps it, and one the mapping reads, one under no owned prefix and
// an entry kept for another reason are not. An extension name holding a slash is
// read unescaped.
func TestUnreadExtensions_ReportsAnOwnedKeyNoSourceReads(t *testing.T) {
	t.Parallel()
	promoter := promotion.New(promotion.ExtensionPromotions{
		Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetStability:              {{Key: "x-acme-stability"}},
			promotion.TargetDeprecationRemovalDate: {{Key: "x-acme-sunset-date"}},
		},
		Prefixes: []string{"x-ac", "x-acme-"},
	})
	diags := promotion.UnreadExtensions(promoter, nil, prefixedDoc())
	got := map[string]string{}
	for _, diagnostic := range diags {
		assert.Equal(t, ir.SeverityWarning, diagnostic.Severity)
		assert.Equal(t, "openapi/unread-extension", diagnostic.Code)
		got[string(diagnostic.Provenance.Pointer)] = diagnostic.Message
	}
	assert.Equal(t, map[string]string{
		"/tags/0/x-acme-stabilty": `extension "x-acme-stabilty" is under prefix "x-acme-", which the promotion policy owns, ` +
			`and the compiler reads no such key; the nearest it reads is "x-acme-stability"`,
		"/s/x-acme-sunset": `extension "x-acme-sunset" is under prefix "x-acme-", which the promotion policy owns, ` +
			`and the compiler reads no such key; the nearest it reads is "x-acme-sunset-date"`,
		"/server/x-acme-s~1tability": `extension "x-acme-s/tability" is under prefix "x-acme-", which the promotion ` +
			`policy owns, and the compiler reads no such key; the nearest it reads is "x-acme-stability"`,
	}, got)
}

// TestUnreadExtensions_NamesNoKeyAMappingDoesNotRead pins the report beside a
// mapping reading no key at all, which has none to suggest, and the policy
// owning no prefix, which reads no document.
func TestUnreadExtensions_NamesNoKeyAMappingDoesNotRead(t *testing.T) {
	t.Parallel()
	empty := promotion.ExtensionPromotions{
		Targets:  map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetStability: nil},
		Prefixes: []string{"x-other"},
	}
	diags := promotion.UnreadExtensions(promotion.New(empty), nil, prefixedDoc())
	require.Len(t, diags, 1)
	assert.Equal(t, `extension "x-other" is under prefix "x-other", which the promotion policy owns, `+
		`and the compiler reads no such key`, diags[0].Message)

	assert.Empty(t, promotion.UnreadExtensions(promotion.New(promotion.ExtensionPromotions{}), nil, prefixedDoc()))
}

// TestUnreadExtensions_TheGroupingKeyIsRead pins a key grouping reads to what
// no prefix reports: given as one grouping reads it is read, and otherwise it
// is a key like any other.
func TestUnreadExtensions_TheGroupingKeyIsRead(t *testing.T) {
	t.Parallel()
	owning := promotion.New(promotion.ExtensionPromotions{
		Targets:  map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetStability: nil},
		Prefixes: []string{"x-other"},
	})
	assert.Empty(t, promotion.UnreadExtensions(owning, []string{"x-other"}, prefixedDoc()))
	assert.Len(t, promotion.UnreadExtensions(owning, nil, prefixedDoc()), 1)
}

// TestUnreadExtensions_TheAliasesKeyIsRead pins the aliases key grouping by
// extension reads beside its key to what no prefix reports, as the key is,
// while a key under the prefix that nothing reads is still reported, naming
// the aliases key as the nearest read. Where grouping reads neither, both are
// reported.
func TestUnreadExtensions_TheAliasesKeyIsRead(t *testing.T) {
	t.Parallel()
	kept := ir.Unmodeled{}
	for _, name := range []string{"x-forge-aliases", "x-forge-alias", "x-forge-group"} {
		unmodeledEntry := vendorExtension(`[]`)
		unmodeledEntry.Provenance.Pointer = jsontext.Pointer("/" + name)
		kept["openapi:"+name] = unmodeledEntry
	}
	doc := &ir.Document{Types: ir.TypeRegistry{"type/s": &ir.Scalar{ID: "type/s", Unmodeled: kept}}}
	owning := promotion.New(promotion.ExtensionPromotions{
		Targets:  map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetStability: nil},
		Prefixes: []string{"x-forge"},
	})
	unread := func(grouping []string) map[string]string {
		got := map[string]string{}
		for _, diagnostic := range promotion.UnreadExtensions(owning, grouping, doc) {
			got[string(diagnostic.Provenance.Pointer)] = diagnostic.Message
		}
		return got
	}

	assert.Equal(t, map[string]string{
		"/x-forge-alias": `extension "x-forge-alias" is under prefix "x-forge", which the promotion policy owns, ` +
			`and the compiler reads no such key; the nearest it reads is "x-forge-aliases"`,
	}, unread([]string{"x-forge-group", "x-forge-aliases"}))
	assert.ElementsMatch(t, []string{"/x-forge-alias", "/x-forge-aliases", "/x-forge-group"},
		slices.Collect(maps.Keys(unread(nil))), "grouping reads none of them")
}
