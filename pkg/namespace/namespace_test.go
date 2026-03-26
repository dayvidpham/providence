package namespace_test

import (
	"errors"
	"testing"

	"github.com/dayvidpham/providence/pkg/namespace"
)

func TestFromGitRemote(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      string
		wantErr   error
	}{
		{
			name:      "https with .git suffix",
			remoteURL: "https://github.com/dayvidpham/providence.git",
			want:      "https://github.com/dayvidpham/providence",
		},
		{
			name:      "scp-style SSH",
			remoteURL: "git@github.com:dayvidpham/providence.git",
			want:      "https://github.com/dayvidpham/providence",
		},
		{
			name:      "https without .git suffix",
			remoteURL: "https://github.com/dayvidpham/providence",
			want:      "https://github.com/dayvidpham/providence",
		},
		{
			name:      "gitlab scp-style SSH",
			remoteURL: "git@gitlab.com:org/sub/repo.git",
			want:      "https://gitlab.com/org/sub/repo",
		},
		{
			name:      "gitlab https",
			remoteURL: "https://gitlab.com/org/sub/repo.git",
			want:      "https://gitlab.com/org/sub/repo",
		},
		{
			name:      "ssh:// protocol",
			remoteURL: "ssh://git@github.com/user/repo.git",
			want:      "https://github.com/user/repo",
		},
		{
			name:      "bitbucket https",
			remoteURL: "https://bitbucket.org/user/repo.git",
			want:      "https://bitbucket.org/user/repo",
		},
		{
			name:      "empty string returns ErrNoRemote",
			remoteURL: "",
			wantErr:   namespace.ErrNoRemote,
		},
		{
			name:      "whitespace-only returns ErrNoRemote",
			remoteURL: "   ",
			wantErr:   namespace.ErrNoRemote,
		},
		{
			name:      "http normalized to https",
			remoteURL: "http://github.com/user/repo.git",
			want:      "https://github.com/user/repo",
		},
		{
			name:      "ssh:// without user prefix",
			remoteURL: "ssh://github.com/user/repo.git",
			want:      "https://github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := namespace.FromGitRemote(tt.remoteURL)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("FromGitRemote(%q) error = %v, want %v", tt.remoteURL, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromGitRemote(%q) unexpected error: %v", tt.remoteURL, err)
			}
			if got != tt.want {
				t.Errorf("FromGitRemote(%q) = %q, want %q", tt.remoteURL, got, tt.want)
			}
		})
	}
}

func TestFromDirectory(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{
			name: "absolute path",
			dir:  "/home/minttea/codebases/dayvidpham/providence",
			want: "file:///home/minttea/codebases/dayvidpham/providence",
		},
		{
			name: "short absolute path",
			dir:  "/tmp",
			want: "file:///tmp",
		},
		{
			name: "root path",
			dir:  "/",
			want: "file:///",
		},
		{
			name: "path with trailing slash cleaned",
			dir:  "/home/user/project/",
			want: "file:///home/user/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := namespace.FromDirectory(tt.dir)
			if got != tt.want {
				t.Errorf("FromDirectory(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

// TestDefaultNamespace is an integration test that verifies DefaultNamespace
// returns the expected value when run inside the providence repository.
func TestDefaultNamespace(t *testing.T) {
	ns, err := namespace.DefaultNamespace()
	if err != nil {
		t.Fatalf("DefaultNamespace() unexpected error: %v", err)
	}

	// When run in the providence repo, should derive from git remote.
	// Accept either the HTTPS remote or a file:// fallback (CI may not have remote).
	if ns == "" {
		t.Error("DefaultNamespace() returned empty string")
	}

	// If we got an HTTPS namespace, verify it looks correct.
	if ns == "https://github.com/dayvidpham/providence" {
		return // exact match
	}

	// Otherwise it should be a valid file:// URI (fallback case).
	if len(ns) > 0 && ns[:7] != "file://" && ns[:8] != "https://" {
		t.Errorf("DefaultNamespace() = %q, expected https:// or file:// URI", ns)
	}
}
