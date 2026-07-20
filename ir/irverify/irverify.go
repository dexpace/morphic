package irverify

import (
	"sort"

	"github.com/dexpace/morphic/ir"
)

// Violation is one structural defect found in an ir.Document: an internal
// compiler bug, never a spec-author problem (that is ir.Diagnostic). Code is a
// stable, slash-namespaced string; Path locates the offending node.
type Violation struct {
	// Code is a stable, slash-namespaced identifier for the defect class.
	Code string
	// Message is a human-readable description of the specific defect.
	Message string
	// Path locates the offending node within the document.
	Path string
}

// Verify runs every structural invariant check over doc and returns the
// violations, sorted by (Code, Path) for deterministic output. A structurally
// sound document yields nil.
func Verify(doc *ir.Document) []Violation {
	if doc == nil {
		return []Violation{{Code: "ir/nil-document", Message: "document is nil"}}
	}

	vs := checkRegistryKeys(doc)
	vs = append(vs, checkReferentialIntegrity(doc)...)
	vs = append(vs, checkNaming(doc)...)

	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Code != vs[j].Code {
			return vs[i].Code < vs[j].Code
		}
		return vs[i].Path < vs[j].Path
	})
	return vs
}

// checkRegistryKeys asserts every entry of each flat, ID-keyed registry
// (Types, Channels, Messages, Auth) is keyed by its own node ID and that the key
// is non-empty (invariant #3). Each registry contributes symmetric empty-*-id and
// *-id-mismatch violations.
func checkRegistryKeys(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		vs = registryKey(vs, "type", "types", string(id), string(td.Common().ID))
	}
	for id, ch := range doc.Channels {
		vs = registryKey(vs, "channel", "channels", string(id), string(ch.ID))
	}
	for id, msg := range doc.Messages {
		vs = registryKey(vs, "message", "messages", string(id), string(msg.ID))
	}
	for id, scheme := range doc.Auth {
		vs = registryKey(vs, "auth", "auth", string(id), string(scheme.ID))
	}
	return vs
}

// registryKey checks one registry entry: key must be non-empty and equal the
// node's own ID. noun is the diagnostic-code singular ("type", "channel", …) and
// reg is the path/message registry label ("types", "channels", …).
func registryKey(vs []Violation, noun, reg, key, nodeID string) []Violation {
	if key == "" {
		return append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: reg + " registry has an empty key",
			Path:    reg + `[""]`,
		})
	}
	if key != nodeID {
		return append(vs, Violation{
			Code:    "ir/" + noun + "-id-mismatch",
			Message: "registry key " + key + " disagrees with node ID " + nodeID,
			Path:    reg + "[" + key + "]",
		})
	}
	return vs
}
