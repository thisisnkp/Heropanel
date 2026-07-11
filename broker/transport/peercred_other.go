//go:build !linux

package transport

import "net"

// peerCredSupported is false where SO_PEERCRED is unavailable. On such platforms
// the server relies on the token and socket file permissions; the uid check is
// skipped. Production runs on Linux, where the check is enforced.
const peerCredSupported = false

func peerCred(_ net.Conn) (uid, gid int, ok bool) { return 0, 0, false }
