package provenance_test

import (
	"testing"

	"github.com/dayvidpham/provenance"
	"github.com/dayvidpham/provenance/pkg/ptypes"
)

// TestProviderIsValid verifies that Provider.IsValid() delegates to the
// bestiary catalog (URD R9). Validation is catalog-membership, not merely
// non-empty-string. These tests exercise the type method directly, since
// the package-level provenance.IsValid and provenance.IsKnown have been
// removed in favour of p.IsValid().
//
// Key properties under test:
//   - Known catalog providers return true (case-sensitive lowercase).
//   - Upper/mixed case of known providers return false (catalog is case-sensitive).
//   - Empty string returns false.
//   - Whitespace-only strings return false.
//   - Non-empty strings absent from the catalog return false.
func TestProviderIsValid(t *testing.T) {
	cases := []struct {
		name  string
		input ptypes.Provider
		want  bool
	}{
		// Known catalog providers — must return true
		{"anthropic lowercase", ptypes.Provider("anthropic"), true},
		{"google lowercase", ptypes.Provider("google"), true},
		{"openai lowercase", ptypes.Provider("openai"), true},
		{"mistral lowercase", ptypes.Provider("mistral"), true},
		{"fireworks-ai lowercase", ptypes.Provider("fireworks-ai"), true},
		// amazon-bedrock and local ARE in the bestiary catalog
		{"amazon-bedrock in catalog", ptypes.Provider("amazon-bedrock"), true},
		{"local in catalog", ptypes.Provider("local"), true},

		// Case-sensitive misses — must return false even though close to known values
		{"ANTHROPIC uppercase", ptypes.Provider("ANTHROPIC"), false},
		{"Anthropic mixed case", ptypes.Provider("Anthropic"), false},
		{"GOOGLE uppercase", ptypes.Provider("GOOGLE"), false},
		{"OpenAI mixed case", ptypes.Provider("OpenAI"), false},

		// Empty and whitespace — must return false
		{"empty string", ptypes.Provider(""), false},
		{"spaces only", ptypes.Provider("   "), false},
		{"tab and spaces", ptypes.Provider("  \t  "), false},

		// Non-empty but not in catalog — must return false
		{"some-future-provider not in catalog", ptypes.Provider("some-future-provider"), false},
		{"nonexistent-vendor not in catalog", ptypes.Provider("nonexistent-vendor"), false},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := c.input.IsValid()
			if got != c.want {
				t.Errorf("Provider(%q).IsValid() = %v, want %v", string(c.input), got, c.want)
			}
		})
	}
}

// TestProviderIsValid_WellKnownConstants verifies the re-exported well-known
// Provider constants are accepted by IsValid() (since they ARE in the
// bestiary catalog).
func TestProviderIsValid_WellKnownConstants(t *testing.T) {
	cases := []struct {
		name string
		p    provenance.Provider
		want bool
	}{
		{"ProviderAnthropic", provenance.ProviderAnthropic, true},
		{"ProviderGoogle", provenance.ProviderGoogle, true},
		{"ProviderOpenAI", provenance.ProviderOpenAI, true},
		// ProviderLocal ("local") is in the bestiary catalog (115 providers).
		{"ProviderLocal", provenance.ProviderLocal, true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := c.p.IsValid()
			if got != c.want {
				t.Errorf("Provider(%q).IsValid() = %v, want %v", string(c.p), got, c.want)
			}
		})
	}
}
