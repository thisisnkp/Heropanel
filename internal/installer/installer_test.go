package installer

import "testing"

func TestParseOSRelease(t *testing.T) {
	content := `NAME="Ubuntu"
VERSION_ID="22.04"
ID=ubuntu
PRETTY_NAME="Ubuntu 22.04.3 LTS"
`
	id, ver, name := parseOSRelease(content)
	if id != "ubuntu" || ver != "22.04" || name != "Ubuntu 22.04.3 LTS" {
		t.Fatalf("parsed (%q,%q,%q)", id, ver, name)
	}
}

func TestParseMemInfo(t *testing.T) {
	content := "MemTotal:        2048000 kB\nMemFree: 100 kB\n"
	if got := parseMemInfo(content); got != 2048000*1024 {
		t.Fatalf("ram = %d", got)
	}
	if got := parseMemInfo("garbage"); got != 0 {
		t.Fatalf("expected 0 for garbage, got %d", got)
	}
}

func TestPkgManagerFor(t *testing.T) {
	cases := map[string]string{"ubuntu": "apt", "debian": "apt", "rocky": "dnf", "almalinux": "dnf", "arch": ""}
	for id, want := range cases {
		if got := pkgManagerFor(id); got != want {
			t.Errorf("pkgManagerFor(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestCompatibility(t *testing.T) {
	good := Profile{Arch: "amd64", OS: "linux", DistroID: "ubuntu", PkgManager: "apt", HasSystemd: true, RAMBytes: 4 << 30}
	if r := Compatibility(good); r.Verdict != VerdictProceed {
		t.Fatalf("good host: %+v", r)
	}

	lowRAM := good
	lowRAM.RAMBytes = 512 << 20
	if r := Compatibility(lowRAM); r.Verdict != VerdictWarn || len(r.Warnings) == 0 {
		t.Fatalf("low RAM should warn: %+v", r)
	}

	noSystemd := good
	noSystemd.HasSystemd = false
	if r := Compatibility(noSystemd); r.Verdict != VerdictBlock {
		t.Fatalf("missing systemd should block: %+v", r)
	}

	badArch := good
	badArch.Arch = "riscv64"
	if r := Compatibility(badArch); r.Verdict != VerdictBlock {
		t.Fatalf("unsupported arch should block: %+v", r)
	}

	badDistro := good
	badDistro.DistroID = "arch"
	badDistro.PkgManager = "pacman"
	if r := Compatibility(badDistro); r.Verdict != VerdictBlock {
		t.Fatalf("unsupported distro should block: %+v", r)
	}
}

func TestPlan(t *testing.T) {
	p := Profile{Arch: "amd64", OS: "linux", DistroID: "ubuntu", PkgManager: "apt", HasSystemd: true}

	full := Plan(p, DefaultOptions())
	if !hasStep(full, "deps.db") || !hasStep(full, "db.provision") {
		t.Fatalf("mariadb plan should provision a DB: %v", ids(full))
	}

	minimal := Plan(p, Options{Minimal: true})
	if hasStep(minimal, "deps.db") || hasStep(minimal, "db.provision") {
		t.Fatalf("minimal plan should not install MariaDB: %v", ids(minimal))
	}
	if !hasStep(minimal, "db.migrate") {
		t.Fatal("even minimal installs run migrations (SQLite)")
	}

	withModules := Plan(p, Options{Modules: []string{"docker"}})
	if !hasStep(withModules, "module.docker") {
		t.Fatalf("module step missing: %v", ids(withModules))
	}
}

func TestNewJournal(t *testing.T) {
	p := Profile{Arch: "amd64", OS: "linux", DistroID: "ubuntu", PkgManager: "apt", HasSystemd: true}
	j := NewJournal("1.0.0", p, DefaultOptions())
	if len(j.Steps) == 0 || j.Steps[0].Status != StatusPending {
		t.Fatalf("journal not initialized: %+v", j.Steps)
	}
	if j.Version != "1.0.0" || j.StartedAt == "" {
		t.Fatalf("journal metadata: %+v", j)
	}
}

func hasStep(steps []Step, id string) bool {
	for _, s := range steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

func ids(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.ID
	}
	return out
}
