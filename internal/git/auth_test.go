package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	c, err := secrets.FromBase64(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return c
}

func authSvc(t *testing.T, repo *fakeRepo, gw *mockGW) *git.Service {
	t.Helper()
	return git.NewService(repo, fakeSites{ref: gitSite()}, gw).WithSecrets(testCipher(t))
}

func TestSetSourceSealsTokenAndNeverReturnsIt(t *testing.T) {
	repo := newFakeRepo()
	svc := authSvc(t, repo, &mockGW{})

	src, err := svc.SetSource(context.Background(), "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", Branch: "main",
		AuthKind: git.AuthToken, Token: "ghp_supersecret",
	})
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	// The API view must never carry the token back out.
	if strings.Contains(src.AuthUsername, "ghp_") || src.PublicKey != "" {
		t.Fatalf("source view leaked credential material: %+v", src)
	}
	if src.AuthKind != git.AuthToken {
		t.Fatalf("auth kind = %q", src.AuthKind)
	}
	// GitHub's token username is not something an operator should have to know.
	if src.AuthUsername != "x-access-token" {
		t.Fatalf("expected the GitHub token username default, got %q", src.AuthUsername)
	}
	// The stored row holds ciphertext, not the token.
	stored, err := repo.GetSourceBySiteID(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.CredentialEnc == "" || strings.Contains(stored.CredentialEnc, "ghp_supersecret") {
		t.Fatalf("token was not sealed at rest: %q", stored.CredentialEnc)
	}
}

func TestTokenUsernameDefaultsPerHost(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://github.com/a/b.git", "x-access-token"},
		{"https://gitlab.com/a/b.git", "oauth2"},
		{"https://bitbucket.org/a/b.git", "x-token-auth"},
		{"https://git.example.com/a/b.git", "git"},
	} {
		svc := authSvc(t, newFakeRepo(), &mockGW{})
		src, err := svc.SetSource(context.Background(), "acme-uid", git.SetSourceInput{
			RepoURL: tc.url, AuthKind: git.AuthToken, Token: "tok",
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		if src.AuthUsername != tc.want {
			t.Fatalf("%s: username = %q, want %q", tc.url, src.AuthUsername, tc.want)
		}
	}
	// An explicit username still wins.
	svc := authSvc(t, newFakeRepo(), &mockGW{})
	src, _ := svc.SetSource(context.Background(), "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/a/b.git", AuthKind: git.AuthToken,
		AuthUsername: "deploybot", Token: "tok",
	})
	if src.AuthUsername != "deploybot" {
		t.Fatalf("explicit username was overridden: %q", src.AuthUsername)
	}
}

// Editing a source (say, changing the branch) must not force the operator to
// re-paste a token they may no longer have a copy of.
func TestUpdateWithoutTokenKeepsTheStoredOne(t *testing.T) {
	repo := newFakeRepo()
	svc := authSvc(t, repo, &mockGW{})
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", AuthKind: git.AuthToken, Token: "tok-1",
	}); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, _ := repo.GetSourceBySiteID(ctx, 1)

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", Branch: "develop", AuthKind: git.AuthToken,
	}); err != nil {
		t.Fatalf("update without token: %v", err)
	}
	after, _ := repo.GetSourceBySiteID(ctx, 1)
	if after.CredentialEnc != before.CredentialEnc {
		t.Fatal("the stored token was replaced by a blank update")
	}
	if after.Branch != "develop" {
		t.Fatalf("branch not updated: %q", after.Branch)
	}

	// A brand-new token source with no token at all is a validation error.
	fresh := authSvc(t, newFakeRepo(), &mockGW{})
	if _, err := fresh.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", AuthKind: git.AuthToken,
	}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation when no token was ever set, got %v", err)
	}
}

func TestSSHKeySourceGeneratesADeployKey(t *testing.T) {
	repo := newFakeRepo()
	svc := authSvc(t, repo, &mockGW{})
	ctx := context.Background()

	src, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "git@github.com:acme/private.git", AuthKind: git.AuthSSHKey,
	})
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if !strings.HasPrefix(src.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("public key not returned for registration: %q", src.PublicKey)
	}
	stored, _ := repo.GetSourceBySiteID(ctx, 1)
	// The private half is sealed and never surfaces in the API view.
	if stored.CredentialEnc == "" || strings.Contains(stored.CredentialEnc, "PRIVATE KEY") {
		t.Fatalf("private key was not sealed: %q", stored.CredentialEnc)
	}

	// Re-saving keeps the key: the operator already registered it on the repo.
	again, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "git@github.com:acme/private.git", Branch: "develop", AuthKind: git.AuthSSHKey,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if again.PublicKey != src.PublicKey {
		t.Fatal("a plain update rotated the deploy key — the repo's registered key would break")
	}

	// Rotation is explicit.
	rotated, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "git@github.com:acme/private.git", AuthKind: git.AuthSSHKey, RotateKey: true,
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.PublicKey == src.PublicKey {
		t.Fatal("rotate_key did not generate a new deploy key")
	}
}

func TestSSHRemoteFormsAccepted(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app.git",
		// Absolute scp-style paths are what self-hosted remotes look like, and
		// rejecting them would lock out every non-GitHub user.
		"git@127.0.0.1:/srv/git/app.git",
		"git@git.example.com:/home/git/repos/app.git",
	} {
		svc := authSvc(t, newFakeRepo(), &mockGW{})
		if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
			RepoURL: u, AuthKind: git.AuthSSHKey,
		}); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
}

// The URL scheme and the auth kind have to agree, or the operator gets an
// opaque "authentication failed" at clone time instead of a clear error now.
func TestRepoURLMustMatchTheAuthKind(t *testing.T) {
	svc := authSvc(t, newFakeRepo(), &mockGW{})
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		in   git.SetSourceInput
	}{
		{"ssh url with no key", git.SetSourceInput{RepoURL: "git@github.com:a/b.git", AuthKind: git.AuthNone}},
		{"ssh url with a token", git.SetSourceInput{RepoURL: "git@github.com:a/b.git", AuthKind: git.AuthToken, Token: "t"}},
		{"https url with a deploy key", git.SetSourceInput{RepoURL: "https://github.com/a/b.git", AuthKind: git.AuthSSHKey}},
		{"credentials in the url", git.SetSourceInput{RepoURL: "https://user:pass@github.com/a/b.git", AuthKind: git.AuthNone}},
		{"unknown auth kind", git.SetSourceInput{RepoURL: "https://github.com/a/b.git", AuthKind: "kerberos"}},
	} {
		if _, err := svc.SetSource(ctx, "acme-uid", tc.in); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation, got %v", tc.name, err)
		}
	}
}

// Without a master key the panel must refuse to store the secret — never fall
// back to plaintext.
func TestPrivateRepoRequiresAMasterKey(t *testing.T) {
	svc := git.NewService(newFakeRepo(), fakeSites{ref: gitSite()}, &mockGW{}) // no WithSecrets
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/a/b.git", AuthKind: git.AuthToken, Token: "tok",
	}); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable without a master key, got %v", err)
	}
	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "git@github.com:a/b.git", AuthKind: git.AuthSSHKey,
	}); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable without a master key, got %v", err)
	}
	// A public repo still works fine.
	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/a/b.git",
	}); err != nil {
		t.Fatalf("public repo should not need a master key: %v", err)
	}
}

// The tri-state matters: an API client that does not know the field yet must
// not silently turn Composer off for an existing source.
func TestAutoComposerDefaultsOnAndOnlyChangesWhenAsked(t *testing.T) {
	repo := newFakeRepo()
	svc := authSvc(t, repo, &mockGW{})
	ctx := context.Background()
	base := git.SetSourceInput{RepoURL: "https://github.com/acme/app.git"}

	src, err := svc.SetSource(ctx, "acme-uid", base)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !src.AutoComposer {
		t.Fatal("a new source should have Composer on by default")
	}

	// Explicitly off.
	off := false
	withOff := base
	withOff.AutoComposer = &off
	if src, err = svc.SetSource(ctx, "acme-uid", withOff); err != nil || src.AutoComposer {
		t.Fatalf("explicit false ignored: %+v err=%v", src, err)
	}

	// An update that omits the field keeps it off.
	changed := base
	changed.Branch = "develop"
	if src, err = svc.SetSource(ctx, "acme-uid", changed); err != nil || src.AutoComposer {
		t.Fatalf("an unrelated update flipped Composer back on: %+v err=%v", src, err)
	}

	// Explicitly back on.
	on := true
	withOn := base
	withOn.AutoComposer = &on
	if src, err = svc.SetSource(ctx, "acme-uid", withOn); err != nil || !src.AutoComposer {
		t.Fatalf("explicit true ignored: %+v err=%v", src, err)
	}
}

func TestDeployPassesAutoComposerToTheBroker(t *testing.T) {
	gw := &mockGW{deployResult: map[string]any{"commit": "abc"}}
	svc := authSvc(t, newFakeRepo(), gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/laravel.git",
	}); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	for _, c := range gw.calls {
		if c.capability == "git.deploy" {
			if c.input["auto_composer"] != true {
				t.Fatalf("auto_composer not passed to the broker: %+v", c.input)
			}
			return
		}
	}
	t.Fatal("git.deploy was not invoked")
}

func TestDeployHandsTheUnsealedCredentialToTheBroker(t *testing.T) {
	gw := &mockGW{deployResult: map[string]any{"commit": "abc123"}}
	svc := authSvc(t, newFakeRepo(), gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", AuthKind: git.AuthToken, Token: "ghp_tok",
	}); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	var deploy *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "git.deploy" {
			deploy = &gw.calls[i]
		}
	}
	if deploy == nil {
		t.Fatal("git.deploy was not invoked")
	}
	auth, ok := deploy.input["auth"].(map[string]any)
	if !ok {
		t.Fatalf("no auth in the deploy payload: %+v", deploy.input)
	}
	if auth["kind"] != git.AuthToken || auth["secret"] != "ghp_tok" || auth["username"] != "x-access-token" {
		t.Fatalf("auth payload = %+v", auth)
	}
}

func TestPublicDeploySendsNoAuth(t *testing.T) {
	gw := &mockGW{deployResult: map[string]any{"commit": "abc123"}}
	svc := authSvc(t, newFakeRepo(), gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/public.git",
	}); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	for _, c := range gw.calls {
		if c.capability == "git.deploy" {
			if _, present := c.input["auth"]; present {
				t.Fatalf("a public repo deploy carried an auth payload: %+v", c.input)
			}
		}
	}
}

// A credential sealed for one site must not decrypt for another: this is the
// AAD binding doing its job end to end, not just in the secrets unit test.
func TestCredentialIsBoundToItsSite(t *testing.T) {
	repo := newFakeRepo()
	cipher := testCipher(t)
	ctx := context.Background()

	site1 := git.NewService(repo, fakeSites{ref: &git.SiteRef{ID: 1, LinuxUser: "hps1",
		HomeDir: "/srv/heropanel/sites/1", DeployMode: "git"}}, &mockGW{}).WithSecrets(cipher)
	if _, err := site1.SetSource(ctx, "site-1", git.SetSourceInput{
		RepoURL: "https://github.com/acme/private.git", AuthKind: git.AuthToken, Token: "tok-1",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stolen, _ := repo.GetSourceBySiteID(ctx, 1)

	// Paste site 1's ciphertext into site 2's row.
	repo.sources[2] = &git.SourceRecord{
		SiteID: 2, RepoURL: "https://github.com/acme/private.git", Branch: "main",
		AuthKind: git.AuthToken, AuthUsername: "x-access-token", CredentialEnc: stolen.CredentialEnc,
	}
	gw := &mockGW{deployResult: map[string]any{"commit": "abc"}}
	site2 := git.NewService(repo, fakeSites{ref: &git.SiteRef{ID: 2, LinuxUser: "hps2",
		HomeDir: "/srv/heropanel/sites/2", DeployMode: "git"}}, gw).WithSecrets(cipher)

	if _, err := site2.RunDeploy(ctx, "site-2", git.TriggerManual, job.Noop); err == nil {
		t.Fatal("site 2 decrypted a credential sealed for site 1")
	}
	for _, c := range gw.calls {
		if c.capability == "git.deploy" {
			t.Fatal("a deploy ran with an unreadable credential")
		}
	}
}
