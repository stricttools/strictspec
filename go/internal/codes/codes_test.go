package codes

import (
	"regexp"
	"testing"
)

func TestCatalogueLoaded(t *testing.T) {
	all := All()
	if len(all) != 130 {
		t.Errorf("catalogue has %d codes, want 130 (must match the appendix and the harness)", len(all))
	}
}

func TestAllIsSortedByCode(t *testing.T) {
	all := All()
	for i := 1; i < len(all); i++ {
		if all[i-1].Code >= all[i].Code {
			t.Errorf("All() not sorted: %q >= %q", all[i-1].Code, all[i].Code)
		}
	}
}

func TestLookup(t *testing.T) {
	e, ok := Lookup("STRICTSPEC_TYPE_NOT_INTEGER")
	if !ok {
		t.Fatal("expected STRICTSPEC_TYPE_NOT_INTEGER in catalogue")
	}
	if e.Area != "TYPE" {
		t.Errorf("area = %q, want TYPE", e.Area)
	}
	if e.Template != "Expected an integer at {path}, got {got}." {
		t.Errorf("template = %q", e.Template)
	}
	if _, ok := Lookup("STRICTSPEC_NOT_A_REAL_CODE"); ok {
		t.Error("Lookup of a bogus code returned ok=true")
	}
}

var codeRe = regexp.MustCompile(`^STRICTSPEC_[A-Z0-9_]+$`)

func TestEveryEntryWellFormed(t *testing.T) {
	areaSet := map[string]bool{}
	for _, a := range Areas {
		areaSet[a] = true
	}
	placeholderRe := regexp.MustCompile(`\{(\w+)\}`)
	for _, e := range All() {
		if !codeRe.MatchString(e.Code) {
			t.Errorf("malformed code %q", e.Code)
		}
		if !areaSet[e.Area] {
			t.Errorf("code %s has area %q outside the closed area set", e.Code, e.Area)
		}
		// Every template placeholder must have a matching slot spec, and every
		// slot spec must be a template placeholder.
		placeholders := map[string]bool{}
		for _, m := range placeholderRe.FindAllStringSubmatch(e.Template, -1) {
			placeholders[m[1]] = true
		}
		specNames := map[string]bool{}
		for _, s := range e.Slots {
			specNames[s.Name] = true
			if !placeholders[s.Name] {
				t.Errorf("code %s: slot %q is not a template placeholder", e.Code, s.Name)
			}
		}
		for name := range placeholders {
			if !specNames[name] {
				t.Errorf("code %s: template placeholder {%s} has no slot spec", e.Code, name)
			}
		}
	}
}

func TestListSlotsHaveElemType(t *testing.T) {
	// Every list slot in the current appendix is list<string>.
	found := false
	for _, e := range All() {
		for _, s := range e.Slots {
			if s.Type == SlotTypeList {
				found = true
				if s.ElemType != SlotTypeString {
					t.Errorf("code %s slot %q: list elem type = %v, want string", e.Code, s.Name, s.ElemType)
				}
			}
		}
	}
	if !found {
		t.Error("expected at least one list<T> slot in the catalogue")
	}
}
