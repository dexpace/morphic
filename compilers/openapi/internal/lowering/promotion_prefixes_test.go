// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// prefixedDoc keeps extensions under the scopes a document writes them in, on
// nodes of three kinds, for UnreadExtensions to find wherever they sit.
func prefixedDoc() *ir.Document {
	at := func(pointer string) ir.UnmodeledEntry {
		e := vendorExtension(`"x"`)
		e.Provenance.Pointer = jsontext.Pointer("/" + pointer)
		return e
	}
	verbatim := at("allOf/0")
	verbatim.Reason = ir.ReasonNoIRHome
	return &ir.Document{
		Unmodeled: ir.Unmodeled{"openapi:tags/0/x-acme-stabilty": at("tags/0/x-acme-stabilty")},
		Types: ir.TypeRegistry{"t/s": &ir.Scalar{ID: "t/s", Unmodeled: ir.Unmodeled{
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
	c := policyCtx(lowering.Policy{Promotions: lowering.ExtensionPromotions{
		Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
			lowering.TargetStability:              {{Key: "x-acme-stability"}},
			lowering.TargetDeprecationRemovalDate: {{Key: "x-acme-sunset-date"}},
		},
		Prefixes: []string{"x-ac", "x-acme-"},
	}})
	diags := lowering.UnreadExtensions(c, prefixedDoc())
	got := map[string]string{}
	for _, d := range diags {
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.Equal(t, "openapi/unread-extension", d.Code)
		got[string(d.Provenance.Pointer)] = d.Message
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
	empty := lowering.ExtensionPromotions{
		Targets:  map[lowering.ExtensionTarget][]lowering.ExtensionSource{lowering.TargetStability: nil},
		Prefixes: []string{"x-other"},
	}
	diags := lowering.UnreadExtensions(policyCtx(lowering.Policy{Promotions: empty}), prefixedDoc())
	require.Len(t, diags, 1)
	assert.Equal(t, `extension "x-other" is under prefix "x-other", which the promotion policy owns, `+
		`and the compiler reads no such key`, diags[0].Message)

	assert.Empty(t, lowering.UnreadExtensions(policyCtx(lowering.Policy{}), prefixedDoc()))
}

// TestUnreadExtensions_TheGroupingKeyIsRead pins the key grouping by extension
// reads to what no prefix reports: under that strategy it is read, and under
// another it is a key like any other.
func TestUnreadExtensions_TheGroupingKeyIsRead(t *testing.T) {
	t.Parallel()
	owning := lowering.ExtensionPromotions{
		Targets:  map[lowering.ExtensionTarget][]lowering.ExtensionSource{lowering.TargetStability: nil},
		Prefixes: []string{"x-other"},
	}
	byExtension := lowering.Policy{Promotions: owning, Grouping: lowering.GroupByExtension,
		GroupingExtension: lowering.GroupingExtension{Key: "x-other", Lists: lowering.ListsAreGroups}}
	assert.Empty(t, lowering.UnreadExtensions(policyCtx(byExtension), prefixedDoc()))
	assert.Len(t, lowering.UnreadExtensions(policyCtx(lowering.Policy{Promotions: owning, Grouping: lowering.GroupByTags}),
		prefixedDoc()), 1)
}

// TestUnreadExtensions_TheAliasesKeyIsRead pins the aliases key grouping by
// extension reads beside its key to what no prefix reports, as the key is,
// while a key under the prefix that nothing reads is still reported, naming
// the aliases key as the nearest read. Under another strategy grouping reads
// neither, so both are reported.
func TestUnreadExtensions_TheAliasesKeyIsRead(t *testing.T) {
	t.Parallel()
	kept := ir.Unmodeled{}
	for _, name := range []string{"x-forge-aliases", "x-forge-alias", "x-forge-group"} {
		e := vendorExtension(`[]`)
		e.Provenance.Pointer = jsontext.Pointer("/" + name)
		kept["openapi:"+name] = e
	}
	doc := &ir.Document{Types: ir.TypeRegistry{"t/s": &ir.Scalar{ID: "t/s", Unmodeled: kept}}}
	owning := lowering.ExtensionPromotions{
		Targets:  map[lowering.ExtensionTarget][]lowering.ExtensionSource{lowering.TargetStability: nil},
		Prefixes: []string{"x-forge"},
	}
	grouping := lowering.GroupingExtension{Key: "x-forge-group", Lists: lowering.ListsAreSegments,
		Aliases: "x-forge-aliases"}
	unread := func(strategy lowering.GroupingStrategy) map[string]string {
		got := map[string]string{}
		policy := lowering.Policy{Promotions: owning, Grouping: strategy, GroupingExtension: grouping}
		for _, d := range lowering.UnreadExtensions(policyCtx(policy), doc) {
			got[string(d.Provenance.Pointer)] = d.Message
		}
		return got
	}

	assert.Equal(t, map[string]string{
		"/x-forge-alias": `extension "x-forge-alias" is under prefix "x-forge", which the promotion policy owns, ` +
			`and the compiler reads no such key; the nearest it reads is "x-forge-aliases"`,
	}, unread(lowering.GroupByExtension))
	for _, strategy := range []lowering.GroupingStrategy{lowering.GroupByTags, lowering.GroupByPathPrefix} {
		assert.ElementsMatch(t, []string{"/x-forge-alias", "/x-forge-aliases", "/x-forge-group"},
			slices.Collect(maps.Keys(unread(strategy))), "under %s grouping reads none of them", strategy)
	}
}

// policyCtx is a context carrying nothing but policy.
func policyCtx(policy lowering.Policy) lowering.Ctx {
	return lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, policy)
}
