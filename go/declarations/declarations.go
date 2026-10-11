// Package declarations is the engine a tool uses to keep declarative TOML
// files of remote resources in a project's repository, compare them with live
// state, import live state into them, and apply them.
//
// A tool supplies its resource types, a live reader, a
// writer, and git; the engine owns the file formats (the strictspec built-in
// schemas declaration-manifest, declaration-project, declaration-claims, and
// declaration-report, and the type definitions declarations.types.toml every
// resource type's schema imports), loading and checking a declaration
// directory, comparing it with live state, the diff identifier, and the order
// and preconditions of apply.
//
// The engine performs no file write and starts no process: Init and
// RenderImport return bytes the tool writes, and git runs through the tool's
// Git.
//
// The declarations of a project live under
// .strictmetadata/<Tool.Directory>/ at its repository root:
// manifest.toml (owner = "<tool>"), project.toml (the project's name),
// claims.toml in the account repository only, and one
// <resource-type>/<name>.toml per resource.
package declarations

import "github.com/stricttools/strictspec/go/strictspec"

// Exit codes of a tool's verify: matching, a failure (an unreadable live
// resource included), and differences found.
const (
	ExitMatching    = 0
	ExitFailed      = 1
	ExitDifferences = 2
)

// Tool is what a tool hands the engine: its name (the owner its manifests
// name), its declaration directory under .strictmetadata/, the command line
// that creates one (the engine appends --project <name> in the refusal that
// names it), and its resource types.
type Tool struct {
	Name          string
	Directory     string
	InitCommand   string
	ResourceTypes []ResourceType
}

// ResourceType is one resource type a tool supplies.
type ResourceType interface {
	// Name is the resource type's directory name, in kebab-case.
	Name() string
	// Schema is the resource type's strictspec schema for
	// strictspec.CompileEmbedded: its files and the main file's name. The
	// engine adds declarations.types.toml to the files.
	Schema() (files map[string]string, main string)
	// Descriptor says what the engine compares, renders, and orders.
	Descriptor() Descriptor
	// Decode binds a schema-valid document to the generic model, normalizing
	// the way the live reader does, and refuses what the schema cannot
	// express, naming the file and the fix. The engine parses the document's
	// unmanaged array and sets File itself.
	Decode(name string, doc strictspec.Value) (Resource, error)
	// Slots are what the declared resource causes elsewhere, without live
	// reads.
	Slots(r Resource) []Slot
	// Derive gives the expected values of the resource's slots, one Derived
	// per slot, reading live state through live where it must.
	Derive(r Resource, live LiveReader) ([]Derived, error)
}

// Descriptor describes a resource type to the engine.
type Descriptor struct {
	// Fields are the top-level compared fields, in rendering order.
	Fields []string
	// Collections are the child collections.
	Collections []Collection
	// Stage orders apply: low first.
	Stage int
	// Exposes are the provider-assigned values a reference may read.
	Exposes []string
	// CreateOnly are top-level fields used only when creating the resource:
	// never compared and never rendered by import. They are not Fields.
	CreateOnly []string
	// DerivableFields are Fields that must be declared unless a derivation
	// supplies them, and must not be declared when one does.
	DerivableFields []string
}

// Collection is one child collection: its name, the fields that identify a
// child, and the compared non-key fields, in rendering order. An item field
// that is neither a key nor a compared field is create-only.
type Collection struct {
	Name   string
	Key    []string
	Fields []string
}

// Value is canonical JSON data (string, int64, bool, []Value, or
// map[string]Value) or a Reference.
type Value any

// Reference is a declared value that is a provider-assigned value of another
// declared resource, such as a server's ipv4. In TOML it is written
// { <resource type with - as _> = "<name>", value = "<exposed value>" }.
type Reference struct {
	ResourceType string
	Name         string
	Value        string
}

// Resource is one resource in the generic model, declared or live. File is
// the declaration file's path relative to the declaration directory
// (<resource type>/<name>.toml), empty for a live resource.
type Resource struct {
	Type      string
	Name      string
	Fields    map[string]Value
	Children  map[string][]Item
	Unmanaged []UnmanagedEntry
	File      string
}

// Item is one child of a resource.
type Item struct {
	Fields map[string]Value
}

// UnmanagedEntry leaves alone every live child of Collection whose fields
// equal all Match values (strings, integers, and booleans).
type UnmanagedEntry struct {
	Collection string
	Match      map[string]Value
	Reason     string
}

// Slot is something a declared resource causes in another declared resource,
// the target: either one top-level field (one of the target's
// DerivableFields), or the children of one collection that match every
// matcher.
type Slot struct {
	ResourceType string
	Name         string
	Field        string
	Collection   string
	Match        map[string]Matcher
}

// Matcher matches one field of a child: an exact value, or a string prefix.
// Build one with Exact or Prefix.
type Matcher struct {
	exact    Value
	prefix   string
	isPrefix bool
}

// Exact matches a field equal to v.
func Exact(v Value) Matcher { return Matcher{exact: v} }

// Prefix matches a string field starting with p.
func Prefix(p string) Matcher { return Matcher{prefix: p, isPrefix: true} }

// Derived is the expected value of one slot: the field's value for a field
// slot, the children for a collection slot, or, when the value does not exist
// yet (the resource causing it is not live), Unresolved saying why.
type Derived struct {
	Slot       Slot
	Value      Value
	Items      []Item
	Unresolved string
}

// LiveReader reads live state; the tool supplies it.
type LiveReader interface {
	// List names every live resource of the type in the account.
	List(resourceType string) ([]string, error)
	// Read reads one resource, normalized as Decode normalizes; false when
	// it is absent.
	Read(resourceType, name string) (Resource, bool, error)
	// Exposed reads a provider-assigned value; false when it does not exist
	// yet.
	Exposed(resourceType, name, value string) (Value, bool, error)
}

// Action is what a Change does.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// Change is one difference apply executes: the creation, update, or deletion
// of a resource (Collection and Field empty), of one child (Collection and Key
// set), or of one field (Field set; Collection and Key too for a child's
// field). Stage is the resource type's.
//
// Resource is the expected resource minus its unresolved values, references
// already resolved: a top-level field waiting on a value that does not exist
// yet is left out; a live child whose key is resolved but some of whose
// fields wait on such a value carries the live child's values of those
// fields; a child that is not live and waits on such a value, or whose key
// does, is left out. Resource.Children holds only the expected children: the
// live children unmanaged entries leave alone, the live children an
// unresolved or failed derivation covers, and the unresolved children are not
// in it, so a writer that replaces a whole set of children must read the
// live set and keep every child the change does not name.
//
// A resource-level delete is the deletion of an unclaimed resource in the
// account repository; its Resource holds only Type and Name, since nothing
// declares the resource.
//
// Item is the expected child as Resource.Children holds it, or the live child
// for a delete.
type Change struct {
	Action       Action
	ResourceType string
	Name         string
	Collection   string
	Key          map[string]Value
	Field        string
	Resource     Resource
	Item         Item
	Stage        int
}

// Writer executes changes; the tool supplies it. It supports a resource-level
// delete for every resource type, or refuses one naming what to do instead.
type Writer interface {
	Apply(Change) error
}

// Git runs git for the engine through the tool's own effects. Status runs
// git -C <root> status --porcelain=v1 --untracked-files=all -- <dir> and
// returns its output.
type Git interface {
	Status(root, dir string) (string, error)
}
