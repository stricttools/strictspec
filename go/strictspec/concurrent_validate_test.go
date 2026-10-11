package strictspec

import (
	"fmt"
	"sync"
	"testing"
)

// Validations running at once share the runtime's compiled-regex cache;
// under -race this test fails if the cache is unguarded. Each program's
// patterns are unique to this test, so every validation writes the cache.
func TestConcurrentValidationsShareTheRegexCacheSafely(t *testing.T) {
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			schema := fmt.Sprintf(`name = "concurrent-%d"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Doc"

[types.Doc]
type = "record"
[types.Doc.fields.a]
type = "string"
required = true
regex = "^concurrent-%d-[a-z]+$"
[types.Doc.fields.b]
type = "string"
required = true
regex = "^concurrent-shared-[0-9]+$"
`, i, i)
			p, err := CompileEmbedded(map[string]string{"s.toml": schema}, "s.toml")
			if err != nil {
				errs <- err
				return
			}
			src := []byte(fmt.Sprintf("format_version = 1\na = \"concurrent-%d-x\"\nb = \"concurrent-shared-1\"\n", i))
			for j := 0; j < 20; j++ {
				if res := p.Validate(src, "toml"); !res.Valid {
					errs <- fmt.Errorf("validation %d: %+v", i, res.Diagnostics)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
