package namespace_test

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/provenance/pkg/namespace"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Fixture types — mirrors testdata/fixtures.yaml structure
// ---------------------------------------------------------------------------

type Fixtures struct {
	FromGitRemote FromGitRemoteFixtures `yaml:"from_git_remote"`
	FromDirectory FromDirectoryFixtures `yaml:"from_directory"`
}

type FromGitRemoteFixtures struct {
	ProtocolForms []ProtocolForm `yaml:"protocol_forms"`
	Hosts         []string       `yaml:"hosts"`
	Paths         []NamedValue   `yaml:"paths"`
	Suffixes      []NamedValue   `yaml:"suffixes"`
	ErrorCases    []ErrorCase    `yaml:"error_cases"`
	ExplicitCases []ExplicitCase `yaml:"explicit_edge_cases"`
}

type ProtocolForm struct {
	Name        string `yaml:"name"`
	Template    string `yaml:"template"`
	Description string `yaml:"description"`
}

type NamedValue struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type ErrorCase struct {
	Name  string `yaml:"name"`
	Input string `yaml:"input"`
}

type ExplicitCase struct {
	Name        string `yaml:"name"`
	Input       string `yaml:"input"`
	Expected    string `yaml:"expected"`
	Description string `yaml:"description"`
}

type FromDirectoryFixtures struct {
	Cases []DirectoryCase `yaml:"cases"`
}

type DirectoryCase struct {
	Name     string `yaml:"name"`
	Input    string `yaml:"input"`
	Expected string `yaml:"expected"`
}

// ---------------------------------------------------------------------------
// Fixture loading
// ---------------------------------------------------------------------------

//go:embed testdata/fixtures.yaml
var fixtureData []byte

func loadFixtures(t *testing.T) Fixtures {
	t.Helper()
	var f Fixtures
	if err := yaml.Unmarshal(fixtureData, &f); err != nil {
		t.Fatalf("failed to parse testdata/fixtures.yaml: %v", err)
	}
	return f
}

// expandTemplate replaces {host}, {path}, {suffix} placeholders in a template.
func expandTemplate(tmpl, host, path, suffix string) string {
	s := strings.ReplaceAll(tmpl, "{host}", host)
	s = strings.ReplaceAll(s, "{path}", path)
	s = strings.ReplaceAll(s, "{suffix}", suffix)
	return s
}

// ---------------------------------------------------------------------------
// FromGitRemote: combinatorial permutation tests
// ---------------------------------------------------------------------------

func TestFromGitRemote_Permutations(t *testing.T) {
	fix := loadFixtures(t)
	fgr := fix.FromGitRemote

	count := 0
	for _, proto := range fgr.ProtocolForms {
		for _, host := range fgr.Hosts {
			for _, path := range fgr.Paths {
				for _, suffix := range fgr.Suffixes {
					input := expandTemplate(proto.Template, host, path.Value, suffix.Value)
					expected := fmt.Sprintf("https://%s/%s", host, path.Value)
					testName := fmt.Sprintf("%s/%s/%s/%s", proto.Name, host, path.Name, suffix.Name)

					t.Run(testName, func(t *testing.T) {
						got, err := namespace.FromGitRemote(input)
						if err != nil {
							t.Fatalf("FromGitRemote(%q) returned unexpected error: %v", input, err)
						}
						if got != expected {
							t.Errorf("FromGitRemote(%q)\n  got:  %q\n  want: %q", input, got, expected)
						}
					})
					count++
				}
			}
		}
	}
	t.Logf("Generated %d permutation test cases (%d protocols × %d hosts × %d paths × %d suffixes)",
		count, len(fgr.ProtocolForms), len(fgr.Hosts), len(fgr.Paths), len(fgr.Suffixes))
}

// ---------------------------------------------------------------------------
// FromGitRemote: error cases
// ---------------------------------------------------------------------------

func TestFromGitRemote_Errors(t *testing.T) {
	fix := loadFixtures(t)

	for _, ec := range fix.FromGitRemote.ErrorCases {
		t.Run(ec.Name, func(t *testing.T) {
			_, err := namespace.FromGitRemote(ec.Input)
			if !errors.Is(err, namespace.ErrNoRemote) {
				t.Errorf("FromGitRemote(%q) error = %v, want ErrNoRemote", ec.Input, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FromGitRemote: explicit edge cases
// ---------------------------------------------------------------------------

func TestFromGitRemote_ExplicitEdgeCases(t *testing.T) {
	fix := loadFixtures(t)

	for _, ec := range fix.FromGitRemote.ExplicitCases {
		t.Run(ec.Name, func(t *testing.T) {
			got, err := namespace.FromGitRemote(ec.Input)
			if err != nil {
				t.Fatalf("FromGitRemote(%q) returned unexpected error: %v\n  case: %s",
					ec.Input, err, ec.Description)
			}
			if got != ec.Expected {
				t.Errorf("FromGitRemote(%q)\n  got:  %q\n  want: %q\n  case: %s",
					ec.Input, got, ec.Expected, ec.Description)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FromDirectory: fixture-driven tests
// ---------------------------------------------------------------------------

func TestFromDirectory_Fixtures(t *testing.T) {
	fix := loadFixtures(t)

	for _, dc := range fix.FromDirectory.Cases {
		t.Run(dc.Name, func(t *testing.T) {
			got := namespace.FromDirectory(dc.Input)
			if got != dc.Expected {
				t.Errorf("FromDirectory(%q) = %q, want %q", dc.Input, got, dc.Expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DefaultNamespace: integration test (runs in real git repo)
// ---------------------------------------------------------------------------

func TestDefaultNamespace_Integration(t *testing.T) {
	ns, err := namespace.DefaultNamespace()
	if err != nil {
		t.Fatalf("DefaultNamespace() unexpected error: %v", err)
	}

	if ns == "" {
		t.Fatal("DefaultNamespace() returned empty string")
	}

	// When run in the provenance repo, should derive from git remote.
	if ns == "https://github.com/dayvidpham/provenance" {
		return // exact match — git remote path
	}

	// Fallback: must be a valid file:// URI.
	if !strings.HasPrefix(ns, "file://") && !strings.HasPrefix(ns, "https://") {
		t.Errorf("DefaultNamespace() = %q, expected https:// or file:// URI", ns)
	}
}
