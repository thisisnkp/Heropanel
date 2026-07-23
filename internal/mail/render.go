package mail

import (
	"sort"
	"strconv"
	"strings"
)

// The renderers turn validated rows into the four flat files the MTAs read.
// Pure functions over already-sorted inputs: the same rows always produce the
// same bytes, so an apply is reproducible and a diff in the broker audit log
// means a real change.

// RenderAccountRow is what rendering needs to know about one account.
type RenderAccountRow struct {
	Domain       string
	LocalPart    string
	PasswordHash string // Dovecot-scheme, e.g. {BLF-CRYPT}$2a$...
	QuotaMB      int
	Active       bool
}

// RenderAliasRow is one alias/forwarder pair.
type RenderAliasRow struct {
	Domain      string
	Source      string // local part
	Destination string // full address
}

// RenderDomains renders postfix virtual_mailbox_domains (hash source).
func RenderDomains(domains []string) string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		out = append(out, d+" OK")
	}
	sort.Strings(out)
	return joinLines(out)
}

// RenderMailboxes renders postfix virtual_mailbox_maps. Every account appears
// regardless of status: a suspended mailbox stops logins (it leaves the users
// file), but keeps RECEIVING — suspending an account must not bounce its mail.
func RenderMailboxes(accounts []RenderAccountRow) string {
	out := make([]string, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, a.LocalPart+"@"+a.Domain+" "+a.Domain+"/"+a.LocalPart+"/Maildir/")
	}
	sort.Strings(out)
	return joinLines(out)
}

// RenderAliases renders postfix virtual_alias_maps, destinations grouped per
// source the way postfix expects ("src dst1,dst2").
func RenderAliases(aliases []RenderAliasRow) string {
	grouped := map[string][]string{}
	for _, a := range aliases {
		src := a.Source + "@" + a.Domain
		grouped[src] = append(grouped[src], a.Destination)
	}
	out := make([]string, 0, len(grouped))
	for src, dsts := range grouped {
		sort.Strings(dsts)
		out = append(out, src+" "+strings.Join(dsts, ","))
	}
	sort.Strings(out)
	return joinLines(out)
}

// RenderUsers renders the dovecot passwd-file: only ACTIVE accounts (removal
// from this file is exactly what "suspended" means), with the per-user quota
// carried as a userdb extra field that overrides the drop-in's 1G default.
//
// Format: user:password:uid:gid:gecos:home:shell:extra — uid/gid/home come
// from the static userdb, so the middle fields stay empty.
func RenderUsers(accounts []RenderAccountRow) string {
	out := make([]string, 0, len(accounts))
	for _, a := range accounts {
		if !a.Active {
			continue
		}
		out = append(out, a.LocalPart+"@"+a.Domain+":"+a.PasswordHash+
			"::::::userdb_quota_rule=*:storage="+strconv.Itoa(a.QuotaMB)+"M")
	}
	sort.Strings(out)
	return joinLines(out)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
