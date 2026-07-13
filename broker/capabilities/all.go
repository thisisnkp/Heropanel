package capabilities

import "github.com/thisisnkp/heropanel/broker/capability"

// All returns every built-in capability. The broker registers these at startup.
// New privileged operations are added here (and gated by policy).
func All() []capability.Capability {
	return []capability.Capability{
		ServiceRestart{},
		SystemUserCreate{},
		SystemUserDelete{},
		SiteCreateDirs{},
		SiteRemoveDirs{},
		WebServerApply{},
		PHPWritePool{},
		DBCreate{},
		DBDrop{},
		DBUserCreate{},
		DBUserDrop{},
		DBGrant{},
		CertInstall{},
		CertWriteChallenge{},
		GitDeploy{},
		GitRollback{},
	}
}
