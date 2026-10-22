package ir

// DomainParents exposes domainParents to ir's external tests.
func (k PrimKind) DomainParents() []PrimKind { return k.domainParents() }
