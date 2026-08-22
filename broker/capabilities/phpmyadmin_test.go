package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// Provisioning writes the sign-on script and a config drop-in, and the config
// puts phpMyAdmin into signon mode pointing at that script.
func TestPHPMyAdminProvisionWritesScriptAndConfig(t *testing.T) {
	fs := fsys.NewFake()
	_ = fs.MkdirAll("/etc/phpmyadmin/conf.d", 0o755)

	res, err := (capabilities.PHPMyAdminProvision{}).Execute(appCtx(&exec.FakeRunner{}, fs),
		raw(t, map[string]any{"redeem_url": "http://127.0.0.1:8443/api/v1/databases/sso/redeem"}))
	if err != nil {
		t.Fatalf("phpmyadmin.provision: %v", err)
	}

	script, ok := fs.Written("/usr/share/phpmyadmin/nexpanel-signon.php")
	if !ok {
		t.Fatal("the sign-on script was not written")
	}
	// phpMyAdmin calls this exact function; a rename here silently disables the
	// whole hand-off with no error anywhere.
	if !strings.Contains(script, "function get_login_credentials(") {
		t.Errorf("the script does not define phpMyAdmin's hook:\n%s", script)
	}
	// It must read the ticket from the query and the credentials from npd's
	// envelope, not from anywhere else.
	for _, want := range []string{"np_ticket", "http://127.0.0.1:8443/api/v1/databases/sso/redeem", "['data']"} {
		if !strings.Contains(script, want) {
			t.Errorf("the script is missing %q", want)
		}
	}

	conf, ok := fs.Written("/etc/phpmyadmin/conf.d/50-nexpanel-signon.php")
	if !ok {
		t.Fatal("the config drop-in was not written")
	}
	// Compared with runs of whitespace collapsed, so aligning the assignments
	// for readability does not break the test.
	flat := strings.Join(strings.Fields(conf), " ")
	for _, want := range []string{
		"['auth_type'] = 'signon';",
		"['SignonScript'] = '/usr/share/phpmyadmin/nexpanel-signon.php';",
		"['AllowNoPassword'] = false;",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the config is missing %q:\n%s", want, conf)
		}
	}
	if res.Data["included"] != true {
		t.Errorf("included = %v; the conf.d directory existed", res.Data["included"])
	}
}

// A distribution with no conf.d directory is reported honestly rather than
// having the drop-in written somewhere nothing reads it.
//
// A file that looks like configuration and is not is worse than an operator
// being told to add one include line: the first fails silently at the moment
// somebody needs it.
func TestPHPMyAdminProvisionReportsAnUnreadConfigDir(t *testing.T) {
	fs := fsys.NewFake()

	res, err := (capabilities.PHPMyAdminProvision{}).Execute(appCtx(&exec.FakeRunner{}, fs),
		raw(t, map[string]any{"redeem_url": "http://127.0.0.1:8443/api/v1/databases/sso/redeem"}))
	if err != nil {
		t.Fatalf("phpmyadmin.provision: %v", err)
	}
	if res.Data["included"] != false {
		t.Errorf("included = %v, want false when no conf.d existed", res.Data["included"])
	}
	if path, _ := res.Data["config"].(string); path == "" {
		t.Error("the config path was not reported, so the operator cannot act on it")
	}
}

// The redeem URL must be loopback.
//
// The sign-on script sends a ticket and receives a database password. Both stay
// on the machine or the design is pointless — so a URL pointing anywhere else
// is a mistake to fail on, not to honour.
func TestPHPMyAdminProvisionRefusesANonLoopbackRedeemURL(t *testing.T) {
	for _, u := range []string{
		"http://203.0.113.7:8443/api/v1/databases/sso/redeem",
		"http://evil.example/api/v1/databases/sso/redeem",
		"https://127.0.0.1:8443/api/v1/databases/sso/redeem", // https to self: not what is wired
		"file:///etc/passwd",
		"",
	} {
		fs := fsys.NewFake()
		if _, err := (capabilities.PHPMyAdminProvision{}).Execute(appCtx(&exec.FakeRunner{}, fs),
			raw(t, map[string]any{"redeem_url": u})); err == nil {
			t.Errorf("redeem URL %q was accepted", u)
		}
		if _, ok := fs.Written("/usr/share/phpmyadmin/nexpanel-signon.php"); ok {
			t.Errorf("redeem URL %q still wrote a script", u)
		}
	}
}
