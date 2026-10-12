package declarations

// Status is what a difference says about one resource, child, or field.
type Status string

const (
	// StatusMissing is declared or derived, not live: apply creates it.
	StatusMissing Status = "missing"
	// StatusUnexpected is live, neither declared, derived, nor unmanaged:
	// apply deletes it.
	StatusUnexpected Status = "unexpected"
	// StatusChanged is one field that differs: apply updates it.
	StatusChanged Status = "changed"
	// StatusUnresolved is an expected value waiting on a provider-assigned
	// value that does not exist yet and that no change of the plan creates
	// (a live server without an IPv6 address): apply does not execute it. A
	// value the plan creates is planned instead: the difference is missing
	// or changed, its value shown by its placeholder.
	StatusUnresolved Status = "unresolved"
	// StatusUnclaimed is, in the account repository, a live account-level
	// resource neither declared there, claimed, nor unmanaged: apply deletes
	// it.
	StatusUnclaimed Status = "unclaimed"
	// StatusMisclaimed is, in a project, a declared resource the claims
	// assign to another project or to none: apply refuses to run.
	StatusMisclaimed Status = "misclaimed"
	// StatusStaleClaim is, in the account repository, a claim a creator
	// wrote (a command that created the resource outside apply) naming a
	// resource that is not live: apply removes the claim. A project's claim
	// of a resource that is not live is none: the project's apply creates
	// the resource.
	StatusStaleClaim Status = "stale-claim"
)

// Report is the result of comparing a declaration directory with live state;
// its JSON (MarshalJSON) is a declaration-report document.
type Report struct {
	Tool        string
	Project     string
	Root        string
	Directory   string
	Matching    bool
	Differences []Difference
	Unmanaged   []UnmanagedMatch
	Unreadable  []Unreadable
}

// Difference is one difference. Collection and Key are set for a child,
// Field for a field; Declared and Live are nil where they do not apply, and
// Source is the declaration file (<resource type>/<name>) the expected value
// comes from, or that causes it, empty where none does.
type Difference struct {
	Status       Status
	ResourceType string
	Name         string
	Collection   string
	Key          map[string]Value
	Field        string
	Declared     Value
	Live         Value
	Source       string
}

// UnmanagedMatch is a live child an unmanaged entry leaves alone, or, in the
// account repository, a live account-level resource claims.toml lists as
// unmanaged (no Collection and no Key).
type UnmanagedMatch struct {
	ResourceType string
	Name         string
	Collection   string
	Key          map[string]Value
	Reason       string
}

// Unreadable is a live read that failed; Name is empty when listing the
// resource type failed.
type Unreadable struct {
	ResourceType string
	Name         string
	Error        string
}
