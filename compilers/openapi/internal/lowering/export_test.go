package lowering

// WithHome exposes setting the Document.Sources index of the document c's
// positions are in, which only InDocument sets, so a test can hand Declared a
// context reading another document's content.
func WithHome(c Ctx, index int) Ctx {
	c.home = index
	return c
}
