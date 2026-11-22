// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

// Details is a set of the details of a field a $ref's referent may lend the
// property declaring it, beneath what the declaration writes itself
// (docs/compilers/openapi.md#ref-use-site). A fold reports no conflict over a
// detail a declaration only borrowed (lastWritten): it set aside nothing the
// declaration wrote.
type Details uint8

// The details a referent may lend. Examples and XML hints stay on the
// referent's node, so no declaration borrows them.
const (
	DetailTitle Details = 1 << iota
	DetailDescription
	DetailExternalDocs
	DetailDeprecation
	DetailDefault
)

// everyDetail is every detail a referent may lend, which a declaration the walk
// cannot read is taken to write.
const everyDetail = DetailTitle | DetailDescription | DetailExternalDocs | DetailDeprecation | DetailDefault

// declarationsWritten is what each declaration of one fold writes itself of
// Details, read once a declaration.
type declarationsWritten struct {
	walk  FieldDeclarationContext
	decls []declaration
	read  map[int]Details
}

// writes reports whether decls[i] writes detail itself rather than borrowing
// it (writtenBy). A detail no referent lends, zero, is always written.
func (w *declarationsWritten) writes(i int, detail Details) bool {
	if detail == 0 {
		return true
	}
	if w.read == nil {
		w.read = make(map[int]Details, len(w.decls))
	}
	written, read := w.read[i]
	if !read {
		written = writtenBy(w.walk, w.decls[i])
		w.read[i] = written
	}
	return written&detail != 0
}

// writtenBy returns what declarationValue writes itself of Details: what the
// declarations a fold settling it kept wrote (declaration.settled), which its
// own pointer does not say, or what walk reads where it is written
// (FieldDeclarationContext.Written), every detail where walk cannot read it.
func writtenBy(walk FieldDeclarationContext, declarationValue declaration) Details {
	if declarationValue.settled {
		return declarationValue.written
	}
	written, known := walk.Written(declarationValue.property)
	if !known {
		return everyDetail
	}
	return written
}
