package repository

import "time"

// tsLayout is the timestamp format shared by both dialects. SQLite stores it as
// TEXT (matching datetime('now')); MariaDB accepts it into DATETIME(6). Values
// are always UTC so lexical (SQLite) and temporal (MariaDB) comparisons agree.
const tsLayout = "2006-01-02 15:04:05"

func fmtTS(t time.Time) string { return t.UTC().Format(tsLayout) }
