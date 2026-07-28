package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Webmail: lay down Roundcube's runtime so the panel's OpenLiteSpeed + PHP can
// serve it against the local Dovecot/Postfix. The application files themselves
// are provisioned out of band (the installer/package places them at
// webmailAppDir); this capability creates the dedicated user, the writable data
// tree, the rendered config, and the sqlite schema — everything that turns the
// static files into a working webmail. No mailbox password is ever handled: a
// user authenticates to Dovecot at login.

const (
	webmailUser    = "webmail"
	webmailAppDir  = "/usr/share/heropanel/roundcube"
	webmailDataDir = "/var/lib/heropanel/webmail"
	webmailConf    = webmailAppDir + "/config/config.inc.php"
)

// WebmailInstall provisions the Roundcube runtime.
type WebmailInstall struct{}

type webmailInstallInput struct {
	Config string `json:"config"` // rendered config.inc.php
}

// Name implements capability.Capability.
func (WebmailInstall) Name() string { return "webmail.install" }

// Execute implements capability.Capability.
func (WebmailInstall) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in webmailInstallInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for webmail.install.")
	}
	if len(in.Config) == 0 || len(in.Config) > 1<<16 || strings.ContainsRune(in.Config, 0) {
		return capability.Result{}, errx.Validation("bad_config", "The webmail config is missing or invalid.")
	}
	// The application files must already be present — this capability configures
	// them, it does not fetch a 40 MB tarball over the wire.
	if ok, _ := c.FS.Exists(webmailAppDir + "/index.php"); !ok {
		return capability.Result{}, errx.New(errx.KindConflict, "roundcube_missing",
			"Roundcube is not installed at "+webmailAppDir+"; install the webmail package first.")
	}

	// 1. The dedicated webmail user (system, nologin). Exit 9 = already exists.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: useraddPath,
		Args: []string{"--system", "--user-group", "--home-dir", webmailDataDir,
			"--no-create-home", "--shell", defaultShell, webmailUser},
		Timeout: 30 * time.Second,
	}); err != nil {
		return capability.Result{}, errx.Upstream(err, "webmail_user_failed", "Could not create the webmail user.")
	} else if res.ExitCode != 0 && res.ExitCode != 9 {
		return capability.Result{}, errx.New(errx.KindUpstream, "webmail_user_failed", "Creating the webmail user failed.")
	}

	// 2. The writable data tree (temp, logs, sqlite db), owned by webmail.
	for _, d := range []string{webmailDataDir, webmailDataDir + "/temp", webmailDataDir + "/logs"} {
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: installPath, Args: []string{"-d", "-m", "0750", "-o", webmailUser, "-g", webmailUser, d},
			Timeout: 20 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "webmail_dirs_failed", "Could not create the webmail data tree.")
		}
	}

	// 3. The rendered config, readable by the webmail group only (it holds the
	// session DES key). config/ lives under the read-only app tree.
	if err := c.FS.WriteFile(webmailConf, []byte(in.Config), 0o640); err != nil {
		return capability.Result{}, errx.Upstream(err, "webmail_conf_failed", "Could not write the webmail config.")
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: chownPath, Args: []string{"root:" + webmailUser, webmailConf}, Timeout: 20 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "webmail_conf_failed", "Could not set the webmail config owner.")
	}

	// 4. The sqlite database. Roundcube auto-initialises a fresh (empty) sqlite
	// file with its schema on first connect, so all we do is create the file
	// owned by the webmail user — running initdb.sh here would race that
	// auto-init and fail with "table already exists". `install /dev/null`
	// creates a 0-byte file with the right owner and mode in one step.
	dbPath := webmailDataDir + "/roundcube.db"
	if ok, _ := c.FS.Exists(dbPath); !ok {
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: installPath, Args: []string{"-m", "0640", "-o", webmailUser, "-g", webmailUser, "/dev/null", dbPath},
			Timeout: 20 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "webmail_db_failed",
				"Could not create the Roundcube database file.")
		}
	}

	return capability.Result{Data: map[string]any{"installed": true, "app_dir": webmailAppDir}}, nil
}

// WebmailStatus reports whether Roundcube is present and configured.
type WebmailStatus struct{}

// Name implements capability.Capability.
func (WebmailStatus) Name() string { return "webmail.status" }

// Execute implements capability.Capability.
func (WebmailStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	app, _ := c.FS.Exists(webmailAppDir + "/index.php")
	conf, _ := c.FS.Exists(webmailConf)
	return capability.Result{Data: map[string]any{"installed": app && conf}}, nil
}
