package strictspec

import "testing"

const lifecycleSample = `format_version = 1

[[lifecycle]]
subject = "portal"
status = "active"
from = 2026-10-07
reason = "first release"

[[licenses]]
subject = "portal"
license = "MIT"
from = 2026-10-07
until = 2027-01-01
reason = "initial license"

[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["v*"]
from = 2026-10-07
reason = "first name"

[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "npm"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"

[[registry_names]]
registry = "npm"
name = "portal"
subject = "portal"
recorded_since = 2026-10-07

[[unversioned_tags]]
tag = "nightly"
reason = "a moving build tag"
recorded = 2026-10-07
`

func TestLifecycleAndLicenseSchemaAcceptsTheSample(t *testing.T) {
	res := LifecycleAndLicenseProgram().Validate([]byte(lifecycleSample), "toml")
	if !res.Valid {
		t.Fatalf("sample refused: %+v", res.Diagnostics)
	}
}

func TestLifecycleAndLicenseSchemaAcceptsTheMinimalRecord(t *testing.T) {
	res := LifecycleAndLicenseProgram().Validate([]byte("format_version = 1\n"), "toml")
	if !res.Valid {
		t.Fatalf("minimal record refused: %+v", res.Diagnostics)
	}
}

func TestLifecycleAndLicenseSchemaRefusals(t *testing.T) {
	cases := map[string]string{
		"missing format version": "[[lifecycle]]\nsubject = \"portal\"\nstatus = \"active\"\nfrom = 2026-10-07\nreason = \"first release\"\n",
		"wrong format version":   "format_version = 2\n",
		"unknown top-level key":  "format_version = 1\ndisclosure = \"public\"\n",
		"unknown status": `format_version = 1
[[lifecycle]]
subject = "portal"
status = "paused"
from = 2026-10-07
reason = "x"
`,
		"string date": `format_version = 1
[[lifecycle]]
subject = "portal"
status = "active"
from = "2026-10-07"
reason = "x"
`,
		"unknown facet": `format_version = 1
[[identities]]
subject = "portal"
facet = "nickname"
value = "p"
registry = ""
tag_patterns = []
from = 2026-10-07
reason = "x"
`,
		"bad license": `format_version = 1
[[licenses]]
subject = "portal"
license = "MIT; rm -rf"
from = 2026-10-07
reason = "x"
`,
		"bad effective version": `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "npm"
tag_patterns = []
effective_version = "next"
reason = "x"
`,
		"empty registry name registry": `format_version = 1
[[registry_names]]
registry = ""
name = "portal"
subject = "portal"
recorded_since = 2026-10-07
`,
	}
	for name, src := range cases {
		res := LifecycleAndLicenseProgram().Validate([]byte(src), "toml")
		if res.Valid {
			t.Errorf("%s: accepted", name)
		}
	}
}
