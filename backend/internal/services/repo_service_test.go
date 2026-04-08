package services

import (
	"testing"
)

// ── nameFromSSHURL ────────────────────────────────────────────────────────────

func TestNameFromSSHURL_Standard(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"git@github.com:org/repo.git", "repo"},
		{"git@github.com:org/my-repo.git", "my-repo"},
		{"git@github.com:org/repo", "repo"},
		{"git@github.com:repo.git", "repo"},
		{"git@bitbucket.org:user/project.git", "project"},
	}
	for _, tc := range cases {
		got, err := nameFromSSHURL(tc.url)
		if err != nil {
			t.Errorf("nameFromSSHURL(%q) unexpected error: %v", tc.url, err)
			continue
		}
		if got != tc.want {
			t.Errorf("nameFromSSHURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestNameFromSSHURL_RejectsPathTraversal(t *testing.T) {
	bad := []string{
		"git@github.com:org/../../../etc/cron.d/evil.git",
		"git@github.com:org/../../evil",
		"git@github.com:../evil.git",
	}
	for _, url := range bad {
		name, err := nameFromSSHURL(url)
		if err == nil {
			t.Errorf("nameFromSSHURL(%q) should have failed but returned %q", url, name)
		}
	}
}

func TestNameFromSSHURL_RejectsUnsafeChars(t *testing.T) {
	bad := []string{
		"git@github.com:org/repo;evil.git",
		"git@github.com:org/repo|cmd.git",
		"git@github.com:org/repo$(evil).git",
		"git@github.com:org/repo name.git", // space
	}
	for _, url := range bad {
		name, err := nameFromSSHURL(url)
		if err == nil {
			t.Errorf("nameFromSSHURL(%q) should have failed but returned %q", url, name)
		}
	}
}

func TestNameFromSSHURL_RejectsDotNames(t *testing.T) {
	for _, url := range []string{
		"git@github.com:org/.git",
		"git@github.com:org/..git",
	} {
		// ".git" suffix is stripped first; resulting name would be "." or ".."
		name, err := nameFromSSHURL(url)
		if err == nil {
			t.Errorf("nameFromSSHURL(%q) should have failed but returned %q", url, name)
		}
	}
}

// ── hostFromSSHURL ────────────────────────────────────────────────────────────

func TestHostFromSSHURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"git@github.com:org/repo.git", "github.com"},
		{"git@bitbucket.org:user/proj.git", "bitbucket.org"},
		{"git@gitlab.company.internal:team/svc.git", "gitlab.company.internal"},
	}
	for _, tc := range cases {
		got := hostFromSSHURL(tc.url)
		if got != tc.want {
			t.Errorf("hostFromSSHURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestHostFromSSHURL_NoAtSign(t *testing.T) {
	if got := hostFromSSHURL("not-an-ssh-url"); got != "" {
		t.Errorf("hostFromSSHURL(no-at) = %q, want empty", got)
	}
}

// ── knownHostsPath ────────────────────────────────────────────────────────────

func TestKnownHostsPath(t *testing.T) {
	got := knownHostsPath("/repos/myrepo")
	want := "/repos/myrepo.known_hosts"
	if got != want {
		t.Errorf("knownHostsPath = %q, want %q", got, want)
	}
}
