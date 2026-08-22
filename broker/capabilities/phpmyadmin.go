package capabilities

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// phpMyAdmin single sign-on.
//
// The panel hands the browser a one-time ticket and nothing else; phpMyAdmin
// redeems it for a throwaway MariaDB account, server-side, over loopback. This
// capability writes the two files that make that work: the signon script
// phpMyAdmin calls, and the config that tells it to.
//
// Why signon rather than posting at the login form. phpMyAdmin's cookie login
// carries a CSRF token, so a blind POST from the panel is unreliable and breaks
// whenever phpMyAdmin tightens it — and it puts a live database password in the
// page. `auth_type = 'signon'` with a `SignonScript` is phpMyAdmin's own
// documented hook for exactly this, and it keeps the password between two
// processes on the same host.
//
// **Honest limit:** the config drop-in is written to the distribution's conf.d
// directory. Debian's phpMyAdmin package reads that directory; the RHEL package
// historically does not, so the result reports whether the directory existed
// and the panel tells the operator to add one include line when it did not.
// Writing into config.inc.php itself is not done: that file belongs to the
// distribution and to the operator, and a panel that rewrites it will one day
// destroy something someone put there.
const (
	pmaSignonScript = "/usr/share/phpmyadmin/nexpanel-signon.php"
	pmaConfDirDeb   = "/etc/phpmyadmin/conf.d"
	pmaConfDirRHEL  = "/etc/phpMyAdmin/conf.d"
	pmaDropInName   = "/50-nexpanel-signon.php"
)

// PHPMyAdminProvision writes the signon script and the config drop-in.
type PHPMyAdminProvision struct{}

type pmaProvisionInput struct {
	// RedeemURL is where the signon script POSTs a ticket. It must be a loopback
	// URL: the whole point is that the credential never crosses a network.
	RedeemURL string `json:"redeem_url"`
}

// Name implements capability.Capability.
func (PHPMyAdminProvision) Name() string { return "phpmyadmin.provision" }

// Execute implements capability.Capability.
func (PHPMyAdminProvision) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in pmaProvisionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for phpmyadmin.provision.")
	}
	if err := validateLoopbackURL(in.RedeemURL); err != nil {
		return capability.Result{}, err
	}

	if err := c.FS.WriteFile(pmaSignonScript, []byte(signonScript(in.RedeemURL)), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "signon_write_failed",
			"Could not write the phpMyAdmin sign-on script.")
	}

	confDir := pmaConfDirRHEL
	if ok, _ := c.FS.Exists(pmaConfDirDeb); ok {
		confDir = pmaConfDirDeb
	}
	// Whether the distribution actually reads this directory is reported rather
	// than assumed. A drop-in nobody includes is a file that looks like
	// configuration and is not, which is worse than an honest "add this line".
	included, _ := c.FS.Exists(confDir)
	if !included {
		if err := c.FS.MkdirAll(confDir, 0o755); err != nil {
			return capability.Result{}, errx.Upstream(err, "conf_dir_failed",
				"Could not create the phpMyAdmin config directory.")
		}
	}
	dropIn := confDir + pmaDropInName
	if err := c.FS.WriteFile(dropIn, []byte(signonConfig()), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "config_write_failed",
			"Could not write the phpMyAdmin config drop-in.")
	}

	return capability.Result{Data: map[string]any{
		"script":   pmaSignonScript,
		"config":   dropIn,
		"included": included,
	}}, nil
}

// validateLoopbackURL refuses anything that is not plain HTTP to this host.
//
// The signon script sends a ticket and receives a database password. Both stay
// on the machine or the design is pointless, so a redeem URL pointing anywhere
// else is a configuration mistake worth failing on rather than honouring.
func validateLoopbackURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" {
		return errx.Validation("bad_redeem_url",
			"The redeem URL must be a plain http:// loopback address.")
	}
	switch u.Hostname() {
	case "127.0.0.1", "::1", "localhost":
		return nil
	}
	return errx.Validation("bad_redeem_url",
		"The redeem URL must point at this host (127.0.0.1), so the credential never leaves it.")
}

// signonConfig is the phpMyAdmin drop-in.
//
// It sets only what signon needs and leaves everything else — host, port,
// blowfish secret, themes — to the distribution's own config.
func signonConfig() string {
	return `<?php
// NexPanel: single sign-on for the panel's database hand-off.
// Written by np-broker (phpmyadmin.provision). Do not edit; it is overwritten.
//
// The panel hands the browser a one-time ticket; the script below redeems it
// against npd over loopback and returns a throwaway MariaDB account scoped to
// one database. No password is ever put in the page.
$i = 1;
$cfg['Servers'][$i]['auth_type']    = 'signon';
$cfg['Servers'][$i]['SignonScript'] = '` + pmaSignonScript + `';
$cfg['Servers'][$i]['host']         = 'localhost';
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
}

// signonScript is the PHP phpMyAdmin calls to obtain credentials.
//
// phpMyAdmin includes this file and calls get_login_credentials(), expecting
// [username, password] or null. Everything it does is bounded: it reads one
// query parameter, POSTs it to a loopback URL baked in at provisioning time,
// and returns what comes back. It has no configuration of its own and no way to
// be pointed somewhere else.
func signonScript(redeemURL string) string {
	return `<?php
/**
 * NexPanel sign-on for phpMyAdmin.
 * Written by np-broker (phpmyadmin.provision). Do not edit; it is overwritten.
 *
 * phpMyAdmin calls get_login_credentials() when auth_type is 'signon'. The
 * browser arrives with ?np_ticket=..., which is a one-time capability to open
 * one database and is worthless without loopback access to the panel. This
 * exchanges it for a throwaway MariaDB account. The password exists only
 * between npd and this process, both on the same host.
 */
function get_login_credentials($cfg_user)
{
    $ticket = isset($_GET['np_ticket']) ? (string) $_GET['np_ticket'] : '';
    if ($ticket === '' || strlen($ticket) > 128) {
        return null;
    }

    $ch = curl_init(` + phpQuote(redeemURL) + `);
    curl_setopt_array($ch, [
        CURLOPT_POST           => true,
        CURLOPT_POSTFIELDS     => json_encode(['ticket' => $ticket]),
        CURLOPT_HTTPHEADER     => ['Content-Type: application/json'],
        CURLOPT_RETURNTRANSFER => true,
        // Short: this is a loopback call to a process on the same machine. A
        // long timeout here would hold a web request open on a panel that is
        // already unhealthy.
        CURLOPT_TIMEOUT        => 5,
        CURLOPT_CONNECTTIMEOUT => 2,
    ]);
    $body = curl_exec($ch);
    $code = curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);

    if ($body === false || $code !== 200) {
        return null;
    }
    $res = json_decode($body, true);
    // npd answers in the standard envelope: the payload is under "data".
    $data = isset($res['data']) ? $res['data'] : null;
    if (!is_array($data) || empty($data['username']) || empty($data['password'])) {
        return null;
    }
    return [$data['username'], $data['password']];
}
`
}

// phpQuote renders a Go string as a single-quoted PHP literal.
//
// The only value passed through it is a loopback URL the broker has already
// validated, so this is belt and braces rather than the boundary — but a string
// written into a file that will be executed is not somewhere to skip escaping.
func phpQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}
