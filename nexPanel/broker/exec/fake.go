package exec

import "context"

// FakeRunner is a test double that records the commands it is asked to run and
// returns a canned result. It lets tests assert the exact argument array a
// capability builds (proving no shell interpolation) without touching the OS.
type FakeRunner struct {
	// Calls records every command passed to Run, in order.
	Calls []Command
	// Result is returned when Fn is nil.
	Result Result
	// Err is returned when Fn is nil.
	Err error
	// Fn, if set, computes the response per call.
	Fn func(Command) (Result, error)
}

// Run implements Runner.
func (f *FakeRunner) Run(_ context.Context, cmd Command) (Result, error) {
	f.Calls = append(f.Calls, cmd)
	if f.Fn != nil {
		return f.Fn(cmd)
	}
	return f.Result, f.Err
}

// Last returns the most recent recorded command and whether one exists.
func (f *FakeRunner) Last() (Command, bool) {
	if len(f.Calls) == 0 {
		return Command{}, false
	}
	return f.Calls[len(f.Calls)-1], true
}
