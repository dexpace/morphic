package ir

// Docs is the human-readable documentation attached to a named entity
// (ir-design §12).
type Docs struct {
	// Summary is a short single-line description.
	Summary string `json:"summary,omitempty"`
	// Description is CommonMark; it may contain {t:TypeID} cross-reference
	// tokens that backends resolve to language-appropriate links.
	Description string `json:"description,omitempty"`
	// ExternalDocs links to supplementary documentation.
	ExternalDocs []Link `json:"externalDocs,omitempty"`
}

// Link is an external documentation reference.
type Link struct {
	// URL is the link target.
	URL string `json:"url,omitempty"`
	// Description labels the link.
	Description string `json:"description,omitempty"`
}

// Deprecation marks an entity as deprecated with optional migration guidance.
type Deprecation struct {
	// Message explains the deprecation and any migration path.
	Message string `json:"message,omitempty"`
	// Since is the version in which the entity was deprecated.
	Since string `json:"since,omitempty"`
	// RemovalVersion is the version in which the entity is scheduled for removal.
	RemovalVersion string `json:"removalVersion,omitempty"`
}
