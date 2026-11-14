// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

import (
	"fmt"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// BodyPath returns the members tokens walk to from the root of every content
// of p, the body of side, and none for the body whole. Each content must walk
// to the same members, since an expression reads one value whichever media
// type arrives. A member is one declaration, so contents reaching the same one
// first walk the same chain from there, and only the first step tells them
// apart: each other content's model is flattened for that step, once per lk
// (#1723). It returns why, and false, where the tokens name no such members.
func BodyPath(lk *Lookups, p *ir.Payload, tokens []string, side string) (segments []ir.PropID, why string, ok bool) {
	if p == nil || len(p.Contents) == 0 {
		return nil, fmt.Sprintf("reads a %s body the document does not describe", side), false
	}
	if len(tokens) == 0 {
		return nil, "", true
	}
	first := &p.Contents[0]
	chain, walked := lk.Walk(first.Type.Target, tokens)
	if !walked {
		return nil, noChain(tokens, side, first.MediaType), false
	}
	for i := 1; i < len(p.Contents); i++ {
		ct := &p.Contents[i]
		member, held := lk.Member(ct.Type.Target, tokens[0])
		if !held {
			return nil, noChain(tokens, side, ct.MediaType), false
		}
		if member.ID != chain[0] {
			return nil, fmt.Sprintf("reads /%s, which the %s body's media types hold as different members",
				strings.Join(tokens, "/"), side), false
		}
	}
	return chain, "", true
}

// noChain is why a body read's tokens walk no chain of members from the root of
// the content of side's body of mediaType.
func noChain(tokens []string, side, mediaType string) string {
	return fmt.Sprintf("reads /%s, which the %s body's %s content holds as no chain of members",
		strings.Join(tokens, "/"), side, mediaType)
}
