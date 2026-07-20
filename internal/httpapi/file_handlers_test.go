package httpapi

import "testing"

// sanitizeFilename feeds a Content-Disposition header, so it must never let a
// path break out of the quoted filename or inject CR/LF header syntax.
func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"pub/index.php":       "index.php",
		"a/b/c/report.pdf":    "report.pdf",
		`ev"il.txt`:           "evil.txt",
		"with\r\nInj: header": "withInj: header", // CR/LF stripped, no header break
		"":                    "download",
		"/":                   "download",
		".":                   "download",
		"deep/../../etc/x":    "x", // only the basename survives
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
