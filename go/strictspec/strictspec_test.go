package strictspec

import "testing"

func TestVersion(t *testing.T) {
	if Version != "0.0.0" {
		t.Fatalf("Version = %q, want %q", Version, "0.0.0")
	}
}
