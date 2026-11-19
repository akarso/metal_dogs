package ports

import (
	"regexp"
	"testing"
)

// Canonical UUID v4: lowercase hex, version nibble 4, RFC 4122 variant 10xx.
// Same shape as internal/platform/id.New (fmt %x of a v4 byte layout).
var canonicalUUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDGenerator_CanonicalV4(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for i := 0; i < 32; i++ {
		id := UUIDGenerator{}.New()
		if !canonicalUUIDv4.MatchString(id) {
			t.Fatalf("id %q is not a canonical UUID v4", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = struct{}{}
	}
}
