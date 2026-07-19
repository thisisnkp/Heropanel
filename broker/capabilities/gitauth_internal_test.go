package capabilities

import "testing"

func TestSSHHostOf(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/app.git":             "github.com",
		"ssh://git@example.org:2222/acme/app.git": "example.org",
		"https://github.com/acme/app.git":         "github.com",
		"git@10.0.0.5:repo.git":                   "10.0.0.5",
	}
	for in, want := range cases {
		if got := sshHostOf(in); got != want {
			t.Errorf("sshHostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKnownHostsContent(t *testing.T) {
	repo := "git@github.com:acme/app.git"

	// A full ssh-keyscan line (host present) is kept verbatim.
	full := "github.com ssh-ed25519 AAAAC3Nz"
	if got := knownHostsContent(full, repo); got != full+"\n" {
		t.Errorf("full line = %q, want verbatim", got)
	}

	// A bare "keytype key" pair is prefixed with the repo host.
	if got := knownHostsContent("ssh-ed25519 AAAAC3Nz", repo); got != "github.com ssh-ed25519 AAAAC3Nz\n" {
		t.Errorf("bare pair = %q, want host-prefixed", got)
	}

	// Comments and blank lines are dropped; multiple valid lines are kept.
	multi := "# github key\ngithub.com ssh-ed25519 AAAA\n\ngithub.com ssh-rsa BBBB\n"
	want := "github.com ssh-ed25519 AAAA\ngithub.com ssh-rsa BBBB\n"
	if got := knownHostsContent(multi, repo); got != want {
		t.Errorf("multi = %q, want %q", got, want)
	}

	// Garbage (single field) yields nothing, so the caller falls back to TOFU.
	if got := knownHostsContent("garbage", repo); got != "" {
		t.Errorf("garbage = %q, want empty", got)
	}
}
