package capabilities_test

import (
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// On a Debian host, system.provision installs the Debian package names and
// enables each component's service.
func TestSystemProvisionDebian(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755) // ⇒ Debian family

	res, err := (capabilities.SystemProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"components": []string{"nginx", "mariadb", "bind"},
	}))
	if err != nil {
		t.Fatalf("system.provision: %v", err)
	}
	if res.Data["distro"] != "debian" {
		t.Errorf("distro = %v, want debian", res.Data["distro"])
	}

	var installed []string
	enabled := map[string]bool{}
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/apt-get" && len(call.Args) > 0 && call.Args[0] == "install" {
			installed = append(installed, call.Args...)
		}
		if call.Path == "/usr/bin/systemctl" && len(call.Args) >= 3 && call.Args[0] == "enable" {
			enabled[call.Args[len(call.Args)-1]] = true
		}
	}
	joined := " " + join(installed) + " "
	for _, pkg := range []string{"nginx", "mariadb-server", "bind9"} {
		if !contains(joined, " "+pkg+" ") {
			t.Errorf("expected %q installed; got %v", pkg, installed)
		}
	}
	for _, svc := range []string{"nginx", "mariadb", "named"} {
		if !enabled[svc] {
			t.Errorf("expected service %q enabled; got %v", svc, enabled)
		}
	}
}

// On a RHEL host (no apt-config), it uses dnf and the RHEL package/service names
// (httpd, not apache2).
func TestSystemProvisionRHEL(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake() // no apt-config ⇒ RHEL family

	_, err := (capabilities.SystemProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"components": []string{"apache", "mysql"},
	}))
	if err != nil {
		t.Fatalf("system.provision (rhel): %v", err)
	}
	var usedDNF bool
	installed := map[string]bool{}
	enabled := map[string]bool{}
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/dnf" && len(call.Args) > 0 && call.Args[0] == "install" {
			usedDNF = true
			for _, a := range call.Args {
				installed[a] = true
			}
		}
		if call.Path == "/usr/bin/systemctl" && len(call.Args) >= 3 && call.Args[0] == "enable" {
			enabled[call.Args[len(call.Args)-1]] = true
		}
	}
	if !usedDNF {
		t.Error("expected dnf to be used on the RHEL family")
	}
	if !installed["httpd"] {
		t.Error("expected httpd (RHEL apache package)")
	}
	if !enabled["httpd"] || !enabled["mysqld"] {
		t.Errorf("expected httpd + mysqld enabled; got %v", enabled)
	}
}

// Provisioning LiteSpeed Enterprise with a license writes the serial file.
func TestSystemProvisionLiteSpeedLicense(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755)

	_, err := (capabilities.SystemProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"components":  []string{"litespeed_enterprise"},
		"license_key": "ABCD-1234-EFGH-5678",
	}))
	if err != nil {
		t.Fatalf("system.provision (lsws license): %v", err)
	}
	got, ok := fs.Written("/usr/local/lsws/conf/serial.no")
	if !ok || !contains(got, "ABCD-1234-EFGH-5678") {
		t.Fatalf("license serial not written: %q", got)
	}
}

// Without a license, no serial file is written (trial).
func TestSystemProvisionLiteSpeedNoLicense(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755)

	if _, err := (capabilities.SystemProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"components": []string{"litespeed_enterprise"},
	})); err != nil {
		t.Fatalf("system.provision (lsws trial): %v", err)
	}
	if _, ok := fs.Written("/usr/local/lsws/conf/serial.no"); ok {
		t.Fatal("no serial file should be written without a license")
	}
}

// An unknown component is refused before anything is installed.
func TestSystemProvisionUnknownComponent(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755)

	_, err := (capabilities.SystemProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"components": []string{"nginx", "iis"},
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown component")
	}
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/apt-get" && len(call.Args) > 0 && call.Args[0] == "install" {
			t.Fatal("no package should be installed when validation fails")
		}
	}
}

// tiny local helpers (avoid importing strings for two checks).
func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
