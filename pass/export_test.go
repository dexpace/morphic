package pass

// CheckDanglingRefs is checkDanglingRefs, for the external tests.
var CheckDanglingRefs = checkDanglingRefs

// CollectTypeIDs is collectTypeIDs, for the external tests.
var CollectTypeIDs = collectTypeIDs

// Families is how many rule families Validate runs, each after an ask of its
// context.
func Families() int { return len(families(nil, nil, false, nil)) }
