package backup

import (
	"bytes"
	"testing"
)

// The wire codec round-trips the field types the SFTP client uses: a packet
// built with byte/uint32/uint64/str/strBytes parses back identically.
func TestSFTPPacketCodec(t *testing.T) {
	var p packet
	p.byte(fxpWrite)
	p.uint32(0xDEADBEEF)
	p.str("a/handle")
	p.uint64(0x0102030405060708)
	p.strBytes([]byte("sealed-bytes"))

	b := p.bytes()
	if b[0] != fxpWrite {
		t.Fatalf("first byte = %d, want %d", b[0], fxpWrite)
	}
	rd := reader{b[1:]} // skip the type byte
	if got := rd.uint32(); got != 0xDEADBEEF {
		t.Errorf("uint32 = %x", got)
	}
	if got := rd.str(); got != "a/handle" {
		t.Errorf("str = %q", got)
	}
	// uint64 is read as two uint32 halves by the request loop's needs; verify the
	// raw bytes are big-endian as SFTP requires.
	hi, lo := rd.uint32(), rd.uint32()
	if hi != 0x01020304 || lo != 0x05060708 {
		t.Errorf("uint64 halves = %x %x", hi, lo)
	}
	if got := rd.strBytes(); !bytes.Equal(got, []byte("sealed-bytes")) {
		t.Errorf("strBytes = %q", got)
	}
}

// A short/truncated reader never panics — it returns zero values, so a malformed
// server reply degrades to an error rather than a crash.
func TestSFTPReaderTruncation(t *testing.T) {
	rd := reader{[]byte{0x00, 0x01}} // fewer than 4 bytes
	if rd.uint32() != 0 {
		t.Error("truncated uint32 should be 0")
	}
	rd2 := reader{[]byte{0x00, 0x00, 0x00, 0x10, 'a'}} // claims 16 bytes, has 1
	if rd2.strBytes() != nil {
		t.Error("truncated strBytes should be nil")
	}
}

// The target derives remote paths under BasePath and defaults sanely.
func TestSFTPTargetPaths(t *testing.T) {
	tgt := NewSFTPTarget(SFTPConfig{Host: "h", User: "u", BasePath: "/backups"})
	if tgt.Name() != TargetSFTP {
		t.Errorf("name = %q", tgt.Name())
	}
	if got := tgt.remotePath("sites/S1/B1.enc"); got != "/backups/sites/S1/B1.enc" {
		t.Errorf("remotePath = %q", got)
	}
	// Defaults: port 22, a base path when none given.
	def := NewSFTPTarget(SFTPConfig{Host: "h"})
	if def.cfg.Port != 22 || def.cfg.BasePath == "" {
		t.Errorf("defaults not applied: %+v", def.cfg)
	}
}
