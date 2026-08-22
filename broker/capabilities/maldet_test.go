package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// maldet output, as it actually looks. The scan id here is the one every
// assertion below depends on: it names the session file the hits are read from,
// so getting it wrong turns every scan into "clean".
const maldetScanOutput = `Linux Malware Detect v1.6.5
            (C) 2002-2023, R-fx Networks <proj@r-fx.org>

maldet(24680): {scan} signatures loaded: 17271 (14895 MD5 | 2376 HEX | 0 USER)
maldet(24680): {scan} building file list for /srv/nexpanel/sites/1, this might take awhile...
maldet(24680): {scan} setting nice scheduler priorities for all operations: cpunice 19 , ionice 6
maldet(24680): {scan} file list completed in 1s, found 4213 files...
maldet(24680): {scan} scan of /srv/nexpanel/sites/1 (4213 files) in progress...
maldet(24680): {scan} 4213/4213 files scanned: 2 hits 0 cleaned
maldet(24680): {scan} scan completed on /srv/nexpanel/sites/1: files 4213, malware hits 2, cleaned 0, time 47s
maldet(24680): {scan} scan report saved, to view run: maldet --report 250822-1531.24680
`

// A scan reads the hits file named by the scan id and returns the detections.
func TestMaldetScanParsesHits(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		return exec.Result{Stdout: []byte(maldetScanOutput)}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/local/sbin/maldet", []byte("#!/bin/sh"), 0o755)
	_ = fs.WriteFile("/usr/local/maldetect/sess/session.hits.250822-1531.24680", []byte(
		"{HEX}php.base64.v23qkr.211 : /srv/nexpanel/sites/1/public/wp-content/x.php\n"+
			"{MD5}php.cmdshell.unclassed.359 : /srv/nexpanel/sites/1/public/shell.php\n"), 0o600)

	res, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fs),
		raw(t, map[string]any{"path": "/srv/nexpanel/sites/1"}))
	if err != nil {
		t.Fatalf("maldet.scan: %v", err)
	}
	if res.Data["scan_id"] != "250822-1531.24680" {
		t.Errorf("scan_id = %v", res.Data["scan_id"])
	}
	if res.Data["infected"] != true {
		t.Errorf("infected = %v, want true", res.Data["infected"])
	}
	findings, _ := res.Data["findings"].([]map[string]any)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want 2", findings)
	}
	if findings[0]["signature"] != "{HEX}php.base64.v23qkr.211" ||
		findings[0]["path"] != "/srv/nexpanel/sites/1/public/wp-content/x.php" {
		t.Errorf("first finding = %+v", findings[0])
	}
}

// maldet's own quarantine must be off. Two quarantines would mean a file the
// operator can see in one and not the other, and a "restore" that puts back
// something the other tool already moved.
func TestMaldetScanForcesQuarantineOff(t *testing.T) {
	var args []string
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		args = c.Args
		return exec.Result{Stdout: []byte(maldetScanOutput)}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/local/sbin/maldet", []byte("#!/bin/sh"), 0o755)

	if _, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fs),
		raw(t, map[string]any{"path": "/srv/nexpanel/sites/1"})); err != nil {
		t.Fatalf("maldet.scan: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"quarantine_hits=0", "quarantine_clean=0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, args)
		}
	}
	// It has to be on the command line, not left to conf.maldet: the host's own
	// config is not something the panel controls or can see.
	if !strings.Contains(joined, "--config-option") {
		t.Errorf("quarantine was left to the host config: %v", args)
	}
}

// No hits file means a clean scan, not a failure. maldet only writes one when
// something matched.
func TestMaldetScanWithNoHitsFileIsClean(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		return exec.Result{Stdout: []byte(maldetScanOutput)}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/local/sbin/maldet", []byte("#!/bin/sh"), 0o755)

	res, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fs),
		raw(t, map[string]any{"path": "/srv/nexpanel/sites/1"}))
	if err != nil {
		t.Fatalf("maldet.scan: %v", err)
	}
	if res.Data["infected"] != false {
		t.Errorf("infected = %v, want false", res.Data["infected"])
	}
	if findings, _ := res.Data["findings"].([]map[string]any); len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}

// Output with no scan id means maldet did not complete a scan. Reporting that
// as clean would be the worst possible failure mode for a malware scanner: a
// green result for a scan that never ran.
func TestMaldetScanWithoutAScanIDFails(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		return exec.Result{Stderr: []byte("maldet: cannot open signature database\n"), ExitCode: 1}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/local/sbin/maldet", []byte("#!/bin/sh"), 0o755)

	if _, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fs),
		raw(t, map[string]any{"path": "/srv/nexpanel/sites/1"})); err == nil {
		t.Fatal("a scan that never ran was reported as a result")
	}
}

// A host without maldet says so, rather than failing somewhere further in.
func TestMaldetScanWithoutMaldetInstalled(t *testing.T) {
	fr := &exec.FakeRunner{}
	res, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"path": "/srv/nexpanel/sites/1"}))
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should say maldet is missing, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("maldet was run despite being absent: %v", fr.Calls)
	}
}

// A path outside the policy roots is refused before maldet runs. A scanner is
// read-only, but "scan this" is still a way to learn what is on the disk.
func TestMaldetScanRefusesAnOutOfRootPath(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/local/sbin/maldet", []byte("#!/bin/sh"), 0o755)

	if _, err := (capabilities.MaldetScan{}).Execute(appCtx(fr, fs),
		raw(t, map[string]any{"path": "/etc"})); err == nil {
		t.Fatal("/etc was accepted as a scan target")
	}
	if len(fr.Calls) != 0 {
		t.Errorf("maldet ran against an out-of-root path: %v", fr.Calls)
	}
}

// ── install ──────────────────────────────────────────────────────────────────

// A download path that is not a plain /downloads/<name>.tar.gz is refused, and
// nothing is fetched.
//
// The host is a constant in the capability, so this is the remaining lever: a
// path with a traversal, a query string or an absolute URL would aim the
// download somewhere the pin does not cover.
func TestMaldetInstallRefusesABadPath(t *testing.T) {
	for _, path := range []string{
		"https://evil.example/x.tar.gz",
		"/downloads/../../etc/passwd",
		"/downloads/maldetect-current.tar.gz?x=1",
		"//evil.example/x.tar.gz",
		"/other/maldetect-current.tar.gz",
		"",
	} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.MaldetInstall{}).Execute(appCtx(fr, fsys.NewFake()),
			raw(t, map[string]any{"path": path})); err == nil {
			t.Errorf("path %q was accepted", path)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("path %q reached the network: %v", path, fr.Calls)
		}
	}
}

// A malformed checksum is refused before the download, not after.
func TestMaldetInstallRefusesABadChecksum(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.MaldetInstall{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{
			"path": "/downloads/maldetect-current.tar.gz", "sha256": "not-a-hash",
		})); err == nil {
		t.Fatal("a malformed checksum was accepted")
	}
	if len(fr.Calls) != 0 {
		t.Errorf("a malformed checksum reached the network: %v", fr.Calls)
	}
}

// A checksum mismatch aborts before anything is unpacked or run.
//
// This is the whole point of pinning: the panel is about to execute the
// downloaded file as root, so the decision has to be made while it is still
// just bytes on disk.
func TestMaldetInstallRefusesAChecksumMismatch(t *testing.T) {
	var ran []string
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path)
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()
	// What "curl" leaves behind. Its hash is not the one asked for below.
	_ = fs.WriteFile("/var/lib/nexpanel/maldet-install/maldetect.tar.gz", []byte("not-the-real-tarball"), 0o600)

	_, err := (capabilities.MaldetInstall{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"path":   "/downloads/maldetect-current.tar.gz",
		"sha256": strings.Repeat("ab", 32),
	}))
	if err == nil {
		t.Fatal("a tarball with the wrong hash was installed")
	}
	for _, p := range ran {
		if strings.Contains(p, "tar") || strings.Contains(p, "sh") {
			t.Errorf("the mismatched download was unpacked or run: %v", ran)
		}
	}
}
