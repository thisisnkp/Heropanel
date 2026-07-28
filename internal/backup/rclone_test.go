package backup

import (
	"strings"
	"testing"
)

// The rclone target joins the remote and key, prefixes the config + verb, and
// disables itself when no remote is set.
func TestRcloneTarget(t *testing.T) {
	if NewRcloneTarget(RcloneConfig{}) != nil {
		t.Error("a target with no remote should be nil (disabled)")
	}
	tgt := NewRcloneTarget(RcloneConfig{Remote: "gdrive:heropanel-backups", Config: "/etc/rclone.conf"})
	if tgt == nil {
		t.Fatal("a configured remote produced a nil target")
	}
	if tgt.Name() != TargetRclone {
		t.Errorf("name = %q", tgt.Name())
	}
	if got := tgt.object("sites/S1/B1.enc"); got != "gdrive:heropanel-backups/sites/S1/B1.enc" {
		t.Errorf("object = %q", got)
	}
	args := tgt.args("rcat", tgt.object("k"))
	joined := strings.Join(args, " ")
	for _, want := range []string{"--config /etc/rclone.conf", "rcat", "gdrive:heropanel-backups/k"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	// Default binary.
	if tgt.cfg.Bin != "rclone" {
		t.Errorf("default bin = %q", tgt.cfg.Bin)
	}
}
