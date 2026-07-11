//go:build linux

package transport

import (
	"net"
	"syscall"
)

// peerCredSupported indicates SO_PEERCRED is available on this platform.
const peerCredSupported = true

// peerCred returns the connecting peer's uid/gid via SO_PEERCRED.
func peerCred(conn net.Conn) (uid, gid int, ok bool) {
	uc, isUnix := conn.(*net.UnixConn)
	if !isUnix {
		return 0, 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	var cred *syscall.Ucred
	var cerr error
	if cerr = raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); cerr != nil {
		return 0, 0, false
	}
	if cred == nil {
		return 0, 0, false
	}
	return int(cred.Uid), int(cred.Gid), true
}
