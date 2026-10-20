package ir

// ClosureSteps reports how many types the supertype walks of f have expanded
// so far. Tests read it to count the work a question cost rather than time it.
func (f *Facts) ClosureSteps() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.steps
}

// AppendContained exposes the containment edges of td to the external tests.
func AppendContained(dst []TypeID, td TypeDef) []TypeID { return appendContained(dst, td) }
