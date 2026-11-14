// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"fmt"
	"iter"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// eachModel calls visit with each model of kind T the walk reaches, and the
// pointer that names it.
func eachModel[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, T) error,
) (jsontext.Pointer, error) {
	return eachSighting(items, what, func(site jsontext.Pointer, _ soa.Locations, m T) error {
		return visit(site, m)
	})
}

// eachSighting is eachModel handing visit the location of each model as well.
//
// The walk and visit run behind runRecovered, so a panic in either becomes an
// error naming what was running. It stops the walk at site, the model being
// visited, or the root when the walk panicked. An error from visit stops it too.
func eachSighting[T any](items iter.Seq[soa.WalkItem], what string,
	visit func(jsontext.Pointer, soa.Locations, T) error,
) (jsontext.Pointer, error) {
	var site jsontext.Pointer
	err := runRecovered(what, func() error {
		for item := range items {
			if err := item.Match(soa.Matcher{Any: func(model any) error {
				m, ok := model.(T)
				if !ok {
					return nil
				}
				site = jsontext.Pointer(item.Location.ToJSONPointer())
				if err := visit(site, item.Location, m); err != nil {
					return err
				}
				site = ""
				return nil
			}}); err != nil {
				return err
			}
		}
		return nil
	})
	return site, err
}

// runRecovered runs f behind diag.Catch and returns its error, or, when it
// panicked, an error naming what was running and quoting the panic. The library
// panics on the caller's goroutine on some shapes its parser accepts
// (docs/parser-library.md#recoverable-panic).
func runRecovered(what string, f func() error) error {
	var err error
	if text, panicked := diag.Catch(func() { err = f() }); panicked {
		return fmt.Errorf("%s panicked: %s", what, text)
	}
	return err
}
