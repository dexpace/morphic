// Package typed answers what the lowering asks of a bind table about the
// typed schemas it holds: where a schema's $ref lands, the schema written
// there and the one its chain ends on. It knows each schema the lowering can
// hold by the position it was read at, and builds a schema once for each
// position a $ref reads as one though the parser's model holds none there.
package typed
