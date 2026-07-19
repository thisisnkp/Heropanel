package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// generateDeployKey creates an ed25519 deploy keypair for a site.
//
// The panel generates the key rather than accepting one from the operator: the
// private half then has exactly one provenance and one home (the sealed
// credential column), and there is no window in which it sits in a browser, a
// clipboard, or a request log. The operator only ever handles the public half,
// which they register as a read-only deploy key on the repository.
//
// ed25519 because every host HeroPanel targets (GitHub, GitLab, Bitbucket)
// accepts it, the keys are small, and there is no key-size decision to get wrong.
//
// It returns the OpenSSH-format private key and the authorized_keys line.
func generateDeployKey(comment string) (privateKey, publicKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", errx.Wrap(err, errx.KindInternal, "keygen_failed", "Could not generate a deploy key.")
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", errx.Wrap(err, errx.KindInternal, "keygen_failed", "Could not encode the deploy key.")
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", errx.Wrap(err, errx.KindInternal, "keygen_failed", "Could not encode the deploy key.")
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		authorized += " " + comment
	}
	return string(pem.EncodeToMemory(block)), authorized, nil
}
