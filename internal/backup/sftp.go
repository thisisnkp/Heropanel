package backup

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// A minimal, dependency-free SFTP-v3 client, just enough to be a backup target:
// upload (create+write), download (open+read), and remove. It speaks the SFTP
// subsystem over an SSH channel HeroPanel already has the transport for
// (x/crypto/ssh, the same library the git deploy keys use) — no third-party
// SFTP dependency, the same lean-deps posture as the hand-rolled SigV4 signer
// and WebAuthn verifier. Sealed blobs are opaque bytes to it; encryption stays
// in the caller. Host-key verification is pinned when a key is configured
// (recommended); an operator who does not pin is warned by the config layer.

// SFTP protocol constants (RFC draft-ietf-secsh-filexfer-02, version 3).
const (
	fxpInit    = 1
	fxpVersion = 2
	fxpOpen    = 3
	fxpClose   = 4
	fxpRead    = 5
	fxpWrite   = 6
	fxpRemove  = 13
	fxpMkdir   = 14
	fxpStatus  = 101
	fxpHandle  = 102
	fxpData    = 103

	fxfRead  = 0x01
	fxfWrite = 0x02
	fxfCreat = 0x08
	fxfTrunc = 0x10

	fxOK  = 0
	fxEOF = 1

	sftpVersion   = 3
	sftpChunk     = 30 * 1024 // under the common 32 KiB packet cap
	sftpDialTO    = 15 * time.Second
	sftpSessionTO = 30 * time.Minute
)

// SFTPConfig configures the SFTP target. Password/PrivateKey come from the
// secret env (never the yaml file), like every other credential.
type SFTPConfig struct {
	Host       string
	Port       int
	User       string
	Password   string // one of Password / PrivateKey
	PrivateKey string // PEM
	BasePath   string // remote directory backups live under
	HostKey    string // pinned host public key (authorized_keys line); "" = insecure
}

// sftpTarget is a backup Target backed by SFTP.
type sftpTarget struct {
	cfg SFTPConfig
}

// NewSFTPTarget builds an SFTP target. It does not connect until first use.
func NewSFTPTarget(cfg SFTPConfig) *sftpTarget {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "heropanel-backups"
	}
	return &sftpTarget{cfg: cfg}
}

// TargetSFTP is the registered name of the SFTP target.
const TargetSFTP = "sftp"

func (*sftpTarget) Name() string { return TargetSFTP }

// remotePath maps an object key to its full remote path under BasePath.
func (t *sftpTarget) remotePath(key string) string {
	return path.Join(t.cfg.BasePath, key)
}

// Put uploads r to the remote key, creating parent directories.
func (t *sftpTarget) Put(ctx context.Context, key string, r io.Reader, _ int64) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	rp := t.remotePath(key)
	if err := c.mkdirAll(path.Dir(rp)); err != nil {
		return err
	}
	return c.writeFile(rp, r)
}

// Get downloads the remote key. The whole object is buffered — backups are
// pulled to a staging file for restore, so a buffered read is fine and keeps
// the SFTP session lifetime short.
func (t *sftpTarget) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	c, err := t.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	buf, err := c.readFile(t.remotePath(key))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf)), nil
}

// Delete removes the remote key. A missing object is not an error (delete is
// idempotent — the caller may be cleaning up a partial write).
func (t *sftpTarget) Delete(ctx context.Context, key string) error {
	c, err := t.dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.remove(t.remotePath(key))
}

// ── the connection ───────────────────────────────────────────────────────────

type sftpConn struct {
	ssh  *ssh.Client
	w    io.WriteCloser
	r    io.Reader
	sess *ssh.Session
	mu   sync.Mutex
	id   uint32
}

func (t *sftpTarget) dial(ctx context.Context) (*sftpConn, error) {
	auth := []ssh.AuthMethod{}
	if t.cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(t.cfg.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("sftp: bad private key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if t.cfg.Password != "" {
		auth = append(auth, ssh.Password(t.cfg.Password))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("sftp: no credentials (password or private key) configured")
	}

	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	var hostKeyAlgos []string
	if t.cfg.HostKey != "" {
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.cfg.HostKey))
		if err != nil {
			return nil, fmt.Errorf("sftp: bad pinned host key: %w", err)
		}
		hostKeyCallback = ssh.FixedHostKey(pk)
		// Force the server to present the pinned key's algorithm, so a host that
		// also offers another key type does not fail the fixed-key comparison.
		hostKeyAlgos = []string{pk.Type()}
	}

	cfg := &ssh.ClientConfig{
		User: t.cfg.User, Auth: auth,
		HostKeyCallback: hostKeyCallback, HostKeyAlgorithms: hostKeyAlgos, Timeout: sftpDialTO,
	}
	addr := net.JoinHostPort(t.cfg.Host, fmt.Sprintf("%d", t.cfg.Port))
	d := net.Dialer{Timeout: sftpDialTO}
	netConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sftp: dial: %w", err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(netConn, addr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("sftp: handshake: %w", err)
	}
	client := ssh.NewClient(sc, chans, reqs)
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sftp: session: %w", err)
	}
	w, err := sess.StdinPipe()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	r, err := sess.StdoutPipe()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sftp: subsystem: %w", err)
	}
	c := &sftpConn{ssh: client, w: w, r: r, sess: sess}
	if err := c.handshake(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return c, nil
}

func (c *sftpConn) Close() error { return c.ssh.Close() }

func (c *sftpConn) nextID() uint32 { c.id++; return c.id }

// handshake performs the SFTP INIT/VERSION exchange.
func (c *sftpConn) handshake() error {
	var p packet
	p.byte(fxpInit)
	p.uint32(sftpVersion)
	if err := c.writePacket(p.bytes()); err != nil {
		return err
	}
	typ, body, err := c.readPacket()
	if err != nil {
		return err
	}
	if typ != fxpVersion {
		return fmt.Errorf("sftp: expected VERSION, got %d", typ)
	}
	_ = body
	return nil
}

func (c *sftpConn) mkdirAll(dir string) error {
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	// Create each ancestor; an existing directory returns a failure status we
	// tolerate (there is no reliable "already exists" code across servers).
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	cur := ""
	if strings.HasPrefix(dir, "/") {
		cur = "/"
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = path.Join(cur, part)
		if strings.HasPrefix(dir, "/") && !strings.HasPrefix(cur, "/") {
			cur = "/" + cur
		}
		_ = c.mkdir(cur) // best-effort per level
	}
	return nil
}

func (c *sftpConn) mkdir(dir string) error {
	id := c.nextID()
	var p packet
	p.byte(fxpMkdir)
	p.uint32(id)
	p.str(dir)
	p.uint32(0) // no attributes
	if err := c.writePacket(p.bytes()); err != nil {
		return err
	}
	return c.expectStatus(id)
}

func (c *sftpConn) writeFile(rp string, r io.Reader) error {
	handle, err := c.open(rp, fxfWrite|fxfCreat|fxfTrunc)
	if err != nil {
		return err
	}
	defer c.close(handle)
	buf := make([]byte, sftpChunk)
	var off uint64
	for {
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			if werr := c.write(handle, off, buf[:n]); werr != nil {
				return werr
			}
			off += uint64(n)
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func (c *sftpConn) readFile(rp string) ([]byte, error) {
	handle, err := c.open(rp, fxfRead)
	if err != nil {
		return nil, err
	}
	defer c.close(handle)
	var out bytes.Buffer
	var off uint64
	for {
		data, eof, err := c.read(handle, off, sftpChunk)
		if err != nil {
			return nil, err
		}
		out.Write(data)
		off += uint64(len(data))
		if eof {
			return out.Bytes(), nil
		}
		if len(data) == 0 {
			return out.Bytes(), nil
		}
	}
}

func (c *sftpConn) open(rp string, pflags uint32) (string, error) {
	id := c.nextID()
	var p packet
	p.byte(fxpOpen)
	p.uint32(id)
	p.str(rp)
	p.uint32(pflags)
	p.uint32(0) // no attrs
	if err := c.writePacket(p.bytes()); err != nil {
		return "", err
	}
	typ, body, err := c.readReply(id)
	if err != nil {
		return "", err
	}
	if typ == fxpHandle {
		rd := reader{body}
		return rd.str(), nil
	}
	return "", statusErr(typ, body)
}

func (c *sftpConn) write(handle string, off uint64, data []byte) error {
	id := c.nextID()
	var p packet
	p.byte(fxpWrite)
	p.uint32(id)
	p.str(handle)
	p.uint64(off)
	p.strBytes(data)
	if err := c.writePacket(p.bytes()); err != nil {
		return err
	}
	return c.expectStatus(id)
}

func (c *sftpConn) read(handle string, off uint64, n uint32) ([]byte, bool, error) {
	id := c.nextID()
	var p packet
	p.byte(fxpRead)
	p.uint32(id)
	p.str(handle)
	p.uint64(off)
	p.uint32(n)
	if err := c.writePacket(p.bytes()); err != nil {
		return nil, false, err
	}
	typ, body, err := c.readReply(id)
	if err != nil {
		return nil, false, err
	}
	if typ == fxpData {
		rd := reader{body}
		return rd.strBytes(), false, nil
	}
	// STATUS: EOF is the normal end of file.
	rd := reader{body}
	code := rd.uint32()
	if code == fxEOF {
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("sftp: read failed (status %d)", code)
}

func (c *sftpConn) close(handle string) error {
	id := c.nextID()
	var p packet
	p.byte(fxpClose)
	p.uint32(id)
	p.str(handle)
	if err := c.writePacket(p.bytes()); err != nil {
		return err
	}
	return c.expectStatus(id)
}

func (c *sftpConn) remove(rp string) error {
	id := c.nextID()
	var p packet
	p.byte(fxpRemove)
	p.uint32(id)
	p.str(rp)
	if err := c.writePacket(p.bytes()); err != nil {
		return err
	}
	// A missing file is fine for an idempotent delete.
	typ, body, err := c.readReply(id)
	if err != nil {
		return err
	}
	rd := reader{body}
	code := rd.uint32()
	if typ == fxpStatus && (code == fxOK) {
		return nil
	}
	// No-such-file (code 2) is tolerated.
	if code == 2 {
		return nil
	}
	return fmt.Errorf("sftp: remove failed (status %d)", code)
}

func (c *sftpConn) expectStatus(id uint32) error {
	typ, body, err := c.readReply(id)
	if err != nil {
		return err
	}
	if typ != fxpStatus {
		return fmt.Errorf("sftp: expected STATUS, got %d", typ)
	}
	rd := reader{body}
	if code := rd.uint32(); code != fxOK {
		return fmt.Errorf("sftp: operation failed (status %d)", code)
	}
	return nil
}

// readReply reads packets until it finds the one whose request id matches.
func (c *sftpConn) readReply(id uint32) (byte, []byte, error) {
	for {
		typ, body, err := c.readPacket()
		if err != nil {
			return 0, nil, err
		}
		if len(body) < 4 {
			return typ, body, nil
		}
		gotID := binary.BigEndian.Uint32(body[:4])
		if gotID == id {
			return typ, body[4:], nil
		}
		// Not ours (shouldn't happen with synchronous use) — keep reading.
	}
}

func statusErr(typ byte, body []byte) error {
	if typ == fxpStatus {
		rd := reader{body}
		return fmt.Errorf("sftp: open failed (status %d)", rd.uint32())
	}
	return fmt.Errorf("sftp: unexpected reply type %d", typ)
}

// ── wire framing ─────────────────────────────────────────────────────────────

func (c *sftpConn) writePacket(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := c.w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.w.Write(payload)
	return err
}

func (c *sftpConn) readPacket() (byte, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > 1<<20 {
		return 0, nil, fmt.Errorf("sftp: bad packet length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return 0, nil, err
	}
	return buf[0], buf[1:], nil
}

// packet builds an SFTP payload.
type packet struct{ b []byte }

func (p *packet) byte(v byte)     { p.b = append(p.b, v) }
func (p *packet) uint32(v uint32) { p.b = binary.BigEndian.AppendUint32(p.b, v) }
func (p *packet) uint64(v uint64) { p.b = binary.BigEndian.AppendUint64(p.b, v) }
func (p *packet) str(s string)    { p.strBytes([]byte(s)) }
func (p *packet) strBytes(v []byte) {
	p.b = binary.BigEndian.AppendUint32(p.b, uint32(len(v)))
	p.b = append(p.b, v...)
}
func (p *packet) bytes() []byte { return p.b }

// reader parses an SFTP payload.
type reader struct{ b []byte }

func (r *reader) uint32() uint32 {
	if len(r.b) < 4 {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[:4])
	r.b = r.b[4:]
	return v
}
func (r *reader) strBytes() []byte {
	n := r.uint32()
	if uint32(len(r.b)) < n {
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}
func (r *reader) str() string { return string(r.strBytes()) }
