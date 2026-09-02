package license

import (
	"io"
	"log/slog"
)

// testLogger keeps the suite's output to the assertions.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
