// Package namespace derives PROV-O namespace URIs from git repository metadata.
//
// A namespace is a URI that scopes providence IDs to a project. For git repos,
// the canonical HTTPS remote URL is used directly (globally unique,
// dereferenceable). For non-git directories, a file:// URI is used.
//
// This package has no dependencies on pkg/ptypes or any internal packages,
// so it can be imported independently by both providence and pasture.
package namespace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoRemote is returned by FromGitRemote when the remote URL is empty.
var ErrNoRemote = errors.New("namespace: no git remote URL")

// DefaultNamespace derives a namespace URI from the current git repo's
// remote URL, falling back to a file:// URI of the working directory.
//
// Derivation order:
//  1. Run "git remote get-url origin" — if successful, normalize to HTTPS URI
//  2. If git fails (not a repo, no origin, git not installed), use file:// URI
//  3. Only returns an error if both strategies fail (e.g., cannot determine cwd)
func DefaultNamespace() (string, error) {
	// Try git remote first.
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err == nil {
		remote := strings.TrimSpace(string(out))
		if ns, err := FromGitRemote(remote); err == nil {
			return ns, nil
		}
	}

	// Fall back to file:// URI of the working directory.
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("namespace: failed to determine working directory: %w — "+
			"git remote was also unavailable, so no namespace could be derived", err)
	}

	absWd, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("namespace: failed to resolve absolute path for %q: %w", wd, err)
	}

	return FromDirectory(absWd), nil
}

// FromGitRemote normalizes a git remote URL to a canonical HTTPS URI.
// Strips .git suffix, converts SSH/git protocol to HTTPS.
//
// Supported formats:
//   - https://github.com/user/repo.git  -> https://github.com/user/repo
//   - git@github.com:user/repo.git      -> https://github.com/user/repo
//   - ssh://git@github.com/user/repo.git -> https://github.com/user/repo
//   - https://github.com/user/repo      -> https://github.com/user/repo (no-op)
//
// Returns ErrNoRemote if remoteURL is empty.
func FromGitRemote(remoteURL string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return "", ErrNoRemote
	}

	var result string

	switch {
	case strings.HasPrefix(remoteURL, "ssh://"):
		// ssh://git@github.com/user/repo.git -> https://github.com/user/repo
		result = remoteURL
		result = strings.TrimPrefix(result, "ssh://")
		// Remove user@ prefix (e.g., "git@")
		if atIdx := strings.Index(result, "@"); atIdx >= 0 {
			result = result[atIdx+1:]
		}
		result = "https://" + result

	case strings.Contains(remoteURL, "@") && strings.Contains(remoteURL, ":") &&
		!strings.Contains(remoteURL, "://"):
		// SCP-style: git@github.com:user/repo.git -> https://github.com/user/repo
		// Split on "@" to get host:path
		atIdx := strings.Index(remoteURL, "@")
		hostPath := remoteURL[atIdx+1:]
		// Replace first ":" with "/" to convert host:path to host/path
		colonIdx := strings.Index(hostPath, ":")
		if colonIdx >= 0 {
			hostPath = hostPath[:colonIdx] + "/" + hostPath[colonIdx+1:]
		}
		result = "https://" + hostPath

	case strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://"):
		// Already an HTTP(S) URL — normalize to https
		result = remoteURL
		if strings.HasPrefix(result, "http://") {
			result = "https://" + strings.TrimPrefix(result, "http://")
		}

	default:
		// Unknown format — return as-is after stripping .git
		result = remoteURL
	}

	// Strip trailing .git suffix
	result = strings.TrimSuffix(result, ".git")

	return result, nil
}

// FromDirectory returns a file:// URI for the given directory path.
// The path is cleaned but not resolved to an absolute path — callers should
// pass an absolute path for a well-formed URI.
func FromDirectory(dir string) string {
	cleaned := filepath.Clean(dir)
	return "file://" + cleaned
}
