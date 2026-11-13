// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// route is what a request the client sends is routed by: its method, and its
// URI template with each expression's name left out (routeShape).
type route struct {
	method, shape string
}

// markSharedRoutes sets SharedRoute on each HTTP binding of an operation the
// groups hold whose route another binding sends too, as paths whose keys
// differ only in a fragment do (docs/compilers/openapi.md#path-keys). Only a
// request the client sends is routed: a webhook has no template and a
// callback's is a runtime expression, so neither is counted.
func (g *serviceGroups) markSharedRoutes() {
	routed := map[route]int{}
	g.eachRoutedBinding(func(_ *ir.HTTPBinding, r route) { routed[r]++ })
	g.eachRoutedBinding(func(hb *ir.HTTPBinding, r route) {
		if routed[r] > 1 {
			hb.SharedRoute = true
		}
	})
}

// eachRoutedBinding calls visit with every HTTP binding of a request the
// client sends that an operation the groups hold carries, and its route.
func (g *serviceGroups) eachRoutedBinding(visit func(*ir.HTTPBinding, route)) {
	for _, id := range g.order {
		ops := g.byID[id].Operations
		for i := range ops {
			for j := range ops[i].Bindings.HTTP {
				if hb := &ops[i].Bindings.HTTP[j]; hb.Inbound == "" {
					visit(hb, route{method: hb.Method, shape: routeShape(hb.URITemplate)})
				}
			}
		}
	}
}

// routeShape returns template with each template expression's name left out,
// read as pathKeyScan reads one: "{", a name holding no brace, then "}". Two
// templates of one shape route the same requests, as OpenAPI says of paths
// that differ only in their expressions' names.
func routeShape(template string) string {
	var b strings.Builder
	b.Grow(len(template))
	for i := 0; i < len(template); i++ {
		b.WriteByte(template[i])
		if template[i] != '{' {
			continue
		}
		if n := strings.IndexAny(template[i+1:], "{}"); n > 0 && template[i+1+n] == '}' {
			b.WriteByte('}')
			i += n + 1
		}
	}
	return b.String()
}
