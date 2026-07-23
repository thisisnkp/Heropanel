package main

import (
	"fmt"
	"os"

	"github.com/thisisnkp/heropanel/pkg/blobcrypt"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

// runDecrypt implements `hpd decrypt <sealed-file> <output-file>`: the
// out-of-band recovery half of the backup pipeline. Every sealed object —
// a site archive (.tar.zst inside), a database dump (.sql.gz inside), a panel
// snapshot (.tar.gz inside) — opens with the same derived key; a wrong key or
// a tampered file refuses with nothing written, exactly like a restore would.
func runDecrypt(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hpd decrypt <sealed-file> <output-file>")
		fmt.Fprintln(os.Stderr, "  HP_SECRET_KEY must hold the panel master key the backup was made under.")
		return 2
	}
	in, out := args[0], args[1]

	master := os.Getenv("HP_SECRET_KEY")
	if master == "" {
		fmt.Fprintln(os.Stderr, "hpd decrypt: HP_SECRET_KEY is not set — the sealed file cannot be opened without the panel master key.")
		return 1
	}
	key, err := secrets.DeriveKeyBase64(master, "backup-v1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "hpd decrypt: bad HP_SECRET_KEY:", err)
		return 1
	}

	src, err := os.Open(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hpd decrypt:", err)
		return 1
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hpd decrypt:", err)
		return 1
	}

	if err := blobcrypt.Open(dst, src, key); err != nil {
		_ = dst.Close()
		_ = os.Remove(out) // never leave a half-written or unauthenticated plaintext
		if err == blobcrypt.ErrCorrupt {
			fmt.Fprintln(os.Stderr, "hpd decrypt: the file failed authentication — wrong key, or the file is corrupt or was tampered with. Nothing was written.")
		} else {
			fmt.Fprintln(os.Stderr, "hpd decrypt:", err)
		}
		return 1
	}
	if err := dst.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "hpd decrypt:", err)
		return 1
	}
	fmt.Println("decrypted:", out)
	return 0
}
