package declarations

import (
	"errors"

	"github.com/stricttools/strictspec/go/strictspec"
)

var errNotImplemented = errors.New("not implemented")

// Set is a loaded declaration directory.
type Set struct {
	Tool      Tool
	Project   Project
	Resources []Resource
}

// Claims is the account repository's claims.toml.
type Claims struct {
	Root      string
	Claims    []Claim
	Unmanaged []UnmanagedResource
}

// Claim assigns an account-level resource to the project declaring it.
type Claim struct {
	ResourceType string
	Name         string
	Project      string
}

// UnmanagedResource is a live account-level resource nobody declares.
type UnmanagedResource struct {
	ResourceType string
	Name         string
	Reason       string
}

type Report struct{}

func ParseReference(v strictspec.Value) (Reference, error)     { return Reference{}, errNotImplemented }
func Load(tool Tool, project Project) (Set, error)             { return Set{}, errNotImplemented }
func LoadClaims(tool Tool, accountRoot string) (Claims, error) { return Claims{}, errNotImplemented }
func Compare(set Set, live LiveReader, claims Claims) (Report, error) {
	return Report{}, errNotImplemented
}
func RenderImport(set Set, live Resource) ([]byte, error) { return nil, errNotImplemented }
func Apply(set Set, live LiveReader, claims Claims, w Writer, g Git, diffID string) (Report, error) {
	return Report{}, errNotImplemented
}
