package capabilities

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Mail: Postfix + Dovecot driven by rendered flat maps, the webserver.apply
// discipline — hpd renders text from validated rows, the broker writes only
// pinned paths, runs fixed commands, and rolls back on failure. Neither MTA
// reads the panel's database: a panel outage never stops mail flow.

// Pinned binaries and paths on the target systems.
const (
	postconfPath = "/usr/sbin/postconf"
	postmapPath  = "/usr/sbin/postmap"
	postfixPath  = "/usr/sbin/postfix"
	doveadmPath  = "/usr/bin/doveadm"

	// mailMapDir holds the postfix hash-map sources (and their .db files).
	mailMapDir = "/etc/postfix/heropanel"
	// dovecotUsersPath is the passwd-file Dovecot authenticates against. Its
	// auth process runs as the dovecot user, so the file is dovecot-owned,
	// mode 0600 — password hashes stay unreadable to everyone else.
	dovecotUsersPath = "/etc/dovecot/heropanel-users"
	// dovecotUser is the unprivileged user dovecot's auth process runs as.
	dovecotUser = "dovecot"
	// dovecotDropin is HeroPanel's dovecot configuration. The 95- prefix makes
	// it sort after the distro defaults, and dovecot's last-wins semantics make
	// it authoritative for the settings it names.
	dovecotDropin = "/etc/dovecot/conf.d/95-heropanel.conf"

	// heropanelBase is the panel's shared /var/lib base; the Maildir root lives
	// beneath it and vmail must be able to traverse into it.
	heropanelBase = "/var/lib/heropanel"
	// vmailUser owns every virtual mailbox; vmailRoot is the Maildir tree.
	vmailUser = "vmail"
	vmailRoot = "/var/lib/heropanel/mail"
)

// mailMapMax bounds a rendered map. The maps are one short line per address;
// a megabyte is tens of thousands of accounts — anything bigger is a bug.
const mailMapMax = 1 << 20

// reMailLocalPart bounds an address's local part (lowercase; the panel
// normalises). Dots/underscores/pluses/hyphens inside, alphanumeric first.
var reMailLocalPart = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)

// dovecotConf is the complete HeroPanel dovecot drop-in. Constant — nothing in
// it is user input. Virtual users authenticate against the rendered
// passwd-file; delivery is LMTP over the postfix-private socket; quotas are
// dovecot's maildir quota with a per-user override carried by the passwd-file.
const dovecotConf = `# HeroPanel mail configuration (rendered; do not edit).
mail_location = maildir:` + vmailRoot + `/%d/%n/Maildir
first_valid_uid = 100
passdb {
  driver = passwd-file
  args = ` + dovecotUsersPath + `
}
userdb {
  driver = static
  args = uid=` + vmailUser + ` gid=` + vmailUser + ` home=` + vmailRoot + `/%d/%n allow_all_users=yes
}
service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    user = postfix
    group = postfix
    mode = 0600
  }
}
mail_plugins = $mail_plugins quota
protocol lmtp {
  mail_plugins = $mail_plugins quota
}
plugin {
  quota = maildir:User quota
  quota_rule = *:storage=1G
}
`

// postconfSettings is the fixed set of main.cf keys mail.provision owns.
// Constant keys AND values — no user input reaches postconf.
var postconfSettings = []string{
	"virtual_mailbox_domains=hash:" + mailMapDir + "/domains",
	"virtual_mailbox_maps=hash:" + mailMapDir + "/mailboxes",
	"virtual_alias_maps=hash:" + mailMapDir + "/aliases",
	"virtual_transport=lmtp:unix:private/dovecot-lmtp",
}

// ── mail.provision ───────────────────────────────────────────────────────────

// MailProvision prepares a host for virtual mail, idempotently: the vmail
// user/group, the Maildir root, the map directory with empty maps, postfix's
// virtual settings, and the dovecot drop-in. Safe to run repeatedly — every
// step is create-if-missing or an idempotent overwrite of panel-owned files.
type MailProvision struct{}

// Name implements capability.Capability.
func (MailProvision) Name() string { return "mail.provision" }

// Execute implements capability.Capability.
func (MailProvision) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	// 1. The vmail user (with its user-private group). Exit 9 = already exists,
	// which is the normal case on every run after the first.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: useraddPath,
		Args: []string{
			"--system", "--user-group",
			"--home-dir", vmailRoot, "--no-create-home",
			"--shell", defaultShell,
			vmailUser,
		},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "vmail_user_failed", "Could not create the vmail user.")
	}
	if res.ExitCode != 0 && res.ExitCode != 9 {
		return capability.Result{}, errx.New(errx.KindUpstream, "vmail_user_failed",
			"Creating the vmail user failed.")
	}

	// 2. Directories: the Maildir root (vmail-owned) and the map dir (root's).
	// The panel base is made world-traversable (0751 — traverse, not list): it
	// is 0750 root by default, which would leave vmail unable to descend into
	// its own Maildir root beneath it. 0751 is the standard mode for a shared
	// /var/lib base and exposes nothing (o+x without o+r).
	for _, cmd := range []exec.Command{
		{Path: installPath, Args: []string{"-d", "-m", "0751", heropanelBase}, Timeout: 20 * time.Second},
		{Path: installPath, Args: []string{"-d", "-m", "0770", "-o", vmailUser, "-g", vmailUser, vmailRoot}, Timeout: 20 * time.Second},
		{Path: installPath, Args: []string{"-d", "-m", "0755", mailMapDir}, Timeout: 20 * time.Second},
	} {
		res, err := c.Runner.Run(c.Ctx, cmd)
		if err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "mail_dirs_failed",
				"Could not create the mail directories.")
		}
	}

	// 3. Empty maps and users file when absent, so both daemons start clean
	// before the first apply. Never overwrite: an apply may already have run.
	for _, name := range []string{"domains", "mailboxes", "aliases"} {
		path := mailMapDir + "/" + name
		if ok, _ := c.FS.Exists(path); !ok {
			if err := c.FS.WriteFile(path, []byte(""), 0o644); err != nil {
				return capability.Result{}, errx.Upstream(err, "mail_map_failed", "Could not seed a mail map.")
			}
		}
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postmapPath, Args: []string{path}, Timeout: 30 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "postmap_failed", "postmap failed on "+name+".")
		}
	}
	if ok, _ := c.FS.Exists(dovecotUsersPath); !ok {
		if err := c.FS.WriteFile(dovecotUsersPath, []byte(""), 0o600); err != nil {
			return capability.Result{}, errx.Upstream(err, "mail_users_failed", "Could not seed the dovecot users file.")
		}
	}
	if err := ownUsersFile(c); err != nil {
		return capability.Result{}, err
	}

	// 4. Postfix virtual settings — fixed keys, fixed values.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: append([]string{"-e"}, postconfSettings...), Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed",
			"Could not apply the postfix virtual settings.")
	}

	// 5. The dovecot drop-in (authoritative via last-wins).
	if err := c.FS.WriteFile(dovecotDropin, []byte(dovecotConf), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "dovecot_conf_failed", "Could not write the dovecot drop-in.")
	}

	// 6. Reload both, tolerating a daemon that is not running yet — provision
	// happens before first start on a fresh host.
	reloaded := reloadMailDaemons(c)
	return capability.Result{Data: map[string]any{"provisioned": true, "reloaded": reloaded}}, nil
}

// ── mail.apply ───────────────────────────────────────────────────────────────

// MailApply writes the full desired mail state — the three postfix maps and
// the dovecot passwd-file — rebuilds the hash maps, and reloads. Declarative
// render-all like webserver.apply, with the same backup/rollback.
type MailApply struct{}

type mailApplyInput struct {
	Domains   string `json:"domains"`
	Mailboxes string `json:"mailboxes"`
	Aliases   string `json:"aliases"`
	Users     string `json:"users"`
}

// Name implements capability.Capability.
func (MailApply) Name() string { return "mail.apply" }

// Execute implements capability.Capability.
func (MailApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.apply.")
	}
	for _, s := range []string{in.Domains, in.Mailboxes, in.Aliases, in.Users} {
		if len(s) > mailMapMax || strings.ContainsRune(s, 0) {
			return capability.Result{}, errx.Validation("bad_input", "A rendered mail map is invalid.")
		}
	}

	backups := map[string]fileBackup{}
	rollback := func() {
		for path, b := range backups {
			if b.existed {
				_ = c.FS.WriteFile(path, b.content, modeFor(path))
			} else {
				_ = c.FS.Remove(path)
			}
		}
		// Best-effort: put the hash maps back in step with the restored sources.
		for _, name := range []string{"domains", "mailboxes", "aliases"} {
			_, _ = c.Runner.Run(c.Ctx, exec.Command{
				Path: postmapPath, Args: []string{mailMapDir + "/" + name}, Timeout: 30 * time.Second,
			})
		}
	}
	write := func(path, content string) error {
		if _, seen := backups[path]; !seen {
			if prev, err := c.FS.ReadFile(path); err == nil {
				backups[path] = fileBackup{existed: true, content: prev}
			} else {
				backups[path] = fileBackup{existed: false}
			}
		}
		return c.FS.WriteFile(path, []byte(content), modeFor(path))
	}

	files := map[string]string{
		mailMapDir + "/domains":   in.Domains,
		mailMapDir + "/mailboxes": in.Mailboxes,
		mailMapDir + "/aliases":   in.Aliases,
		dovecotUsersPath:          in.Users,
	}
	// Deterministic order (maps then users) keeps the audit log stable.
	for _, path := range []string{mailMapDir + "/domains", mailMapDir + "/mailboxes", mailMapDir + "/aliases", dovecotUsersPath} {
		if err := write(path, files[path]); err != nil {
			rollback()
			return capability.Result{}, errx.Upstream(err, "mail_write_failed", "Could not write a mail map.")
		}
	}
	for _, name := range []string{"domains", "mailboxes", "aliases"} {
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postmapPath, Args: []string{mailMapDir + "/" + name}, Timeout: 30 * time.Second,
		})
		if err != nil || res.ExitCode != 0 {
			rollback()
			return capability.Result{}, errx.New(errx.KindUpstream, "postmap_failed",
				"postmap failed on "+name+"; changes were rolled back.")
		}
	}
	// A rewrite leaves the file root-owned again; hand it back to dovecot.
	if err := ownUsersFile(c); err != nil {
		rollback()
		return capability.Result{}, err
	}

	reloaded := reloadMailDaemons(c)
	return capability.Result{Data: map[string]any{"applied": true, "reloaded": reloaded}}, nil
}

// ownUsersFile hands the passwd-file to dovecot's auth user, private.
func ownUsersFile(c capability.Context) error {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: chownPath, Args: []string{dovecotUser + ":" + dovecotUser, dovecotUsersPath},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "mail_users_failed",
			"Could not hand the dovecot users file to dovecot.")
	}
	return nil
}

// modeFor keeps the dovecot passwd-file (password hashes) root-only while the
// postfix map sources stay world-readable like postfix expects.
func modeFor(path string) fs.FileMode {
	if path == dovecotUsersPath {
		return 0o600
	}
	return 0o644
}

// reloadMailDaemons reloads postfix and dovecot, tolerating either not
// running (provision-before-first-start; a stopped daemon picks the files up
// on boot). Reports what actually reloaded.
func reloadMailDaemons(c capability.Context) map[string]bool {
	out := map[string]bool{"postfix": false, "dovecot": false}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postfixPath, Args: []string{"reload"}, Timeout: 30 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		out["postfix"] = true
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: doveadmPath, Args: []string{"reload"}, Timeout: 30 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		out["dovecot"] = true
	}
	return out
}

// ── mail.purge ───────────────────────────────────────────────────────────────

// MailPurge deletes stored mail — one mailbox's Maildir, or a whole domain's
// tree. The path is DERIVED from a validated domain (and optional local part),
// never taken from input, so nothing outside the vmail root can be named.
type MailPurge struct{}

type mailPurgeInput struct {
	Domain    string `json:"domain"`
	LocalPart string `json:"local_part,omitempty"` // empty = the whole domain
}

// Name implements capability.Capability.
func (MailPurge) Name() string { return "mail.purge" }

// Execute implements capability.Capability.
func (MailPurge) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailPurgeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.purge.")
	}
	if err := capability.ValidateFQDN(in.Domain); err != nil {
		return capability.Result{}, err
	}
	path := vmailRoot + "/" + in.Domain
	if in.LocalPart != "" {
		if !reMailLocalPart.MatchString(in.LocalPart) {
			return capability.Result{}, errx.Validation("invalid_local_part", "Invalid mailbox name.")
		}
		path += "/" + in.LocalPart
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: rmPath, Args: []string{"-rf", "--", path}, Timeout: 5 * time.Minute,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "mail_purge_failed",
			"Could not remove the stored mail.")
	}
	return capability.Result{Data: map[string]any{"purged": path}}, nil
}
