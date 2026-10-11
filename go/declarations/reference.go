package declarations

import "strings"

// referenceKey is the TOML key a reference to a resource type is written
// under: the resource type's name with - as _.
func referenceKey(resourceType string) string { return strings.ReplaceAll(resourceType, "-", "_") }
