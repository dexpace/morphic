// Package anchors indexes the $dynamicAnchor declarations of a source
// document, which a $dynamicRef expands against. It reads the tree as the
// parser does, through aliases and `<<` merges: an anchor a merge or an alias
// pulls into a position is declared there as well, so "declared exactly once"
// counts what the document declares rather than what it spells out.
package anchors
