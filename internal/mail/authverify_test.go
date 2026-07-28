package mail

import (
	"strings"
	"testing"
)

// The SPF policy render maps enforce → hard reject and monitor → header-only,
// while never rejecting on PermError/TempError (a broken sender zone must not
// bounce good mail).
func TestRenderPolicydSPFConf(t *testing.T) {
	enforce := RenderPolicydSPFConf(true)
	if !strings.Contains(enforce, "Mail_From_reject = Fail") || !strings.Contains(enforce, "TestOnly = 0") {
		t.Errorf("enforce SPF config wrong:\n%s", enforce)
	}
	monitor := RenderPolicydSPFConf(false)
	if !strings.Contains(monitor, "Mail_From_reject = False") || !strings.Contains(monitor, "TestOnly = 1") {
		t.Errorf("monitor SPF config wrong:\n%s", monitor)
	}
	for _, cfg := range []string{enforce, monitor} {
		if !strings.Contains(cfg, "PermError_reject = False") || !strings.Contains(cfg, "TempError_Defer = False") {
			t.Errorf("SPF config would bounce on a transient/permanent DNS error:\n%s", cfg)
		}
	}
}

// The DMARC render carries the AuthservID and flips RejectFailures with the
// mode, and always ignores authenticated (local) clients so panel outbound is
// never DMARC-rejected.
func TestRenderOpenDMARCConf(t *testing.T) {
	enforce := RenderOpenDMARCConf("mail.example.com", true)
	if !strings.Contains(enforce, "AuthservID mail.example.com") {
		t.Errorf("DMARC config missing AuthservID:\n%s", enforce)
	}
	if !strings.Contains(enforce, "RejectFailures true") {
		t.Errorf("enforce DMARC config should reject:\n%s", enforce)
	}
	monitor := RenderOpenDMARCConf("mail.example.com", false)
	if !strings.Contains(monitor, "RejectFailures false") {
		t.Errorf("monitor DMARC config should not reject:\n%s", monitor)
	}
	for _, cfg := range []string{enforce, monitor} {
		if !strings.Contains(cfg, "IgnoreAuthenticatedClients true") {
			t.Errorf("DMARC config would reject authenticated local submission:\n%s", cfg)
		}
	}
}
