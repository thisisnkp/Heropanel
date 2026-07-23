package broker

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Live log streaming: `docker logs --follow` behind the same connection upgrade
// the terminal and container-exec use.
//
// This is deliberately the *one-way* twin of container exec. A shell needs a
// pseudo-terminal and a bidirectional pump; a log follow needs neither — it only
// ever produces output — so it runs as a plain child process whose stdout and
// stderr are interleaved into a single pipe, and the transport streams that pipe
// to the client until either side hangs up.
//
// Authorization matches the *polled* logs capability, not exec: reading a
// container's logs is not restricted to containers the panel manages (an admin
// diagnosing a host must be able to read the logs of whatever is misbehaving),
// so there is no ownership check here. It is still policy-gated and audited,
// because a container's logs routinely carry connection strings and tokens, and
// following them is the same disclosure the polled read is.

// CapContainerLogsFollow is the policy name gating live log streaming.
const CapContainerLogsFollow = "docker.container.logs.follow"

// maxFollowTail bounds the backlog a follow starts with. The follow itself is
// unbounded in time — that is the point — but the initial catch-up is clamped so
// a week-old crash loop does not flood the client with gigabytes before the first
// live line arrives.
const maxFollowTail = 2000

// LogFollowRequest asks to stream a container's logs live.
type LogFollowRequest struct {
	Container  string
	Tail       int
	Timestamps bool
	Actor      capability.Actor
}

// LogStream is a running `docker logs --follow`. Read yields interleaved
// stdout+stderr as the container writes it; Close stops the follow and reaps the
// process. It is the one-way analogue of pty.Session.
type LogStream struct {
	cmd  *exec.Cmd
	pr   *os.File
	once sync.Once
	code int
}

// Read returns the next chunk of the container's output.
func (l *LogStream) Read(p []byte) (int, error) { return l.pr.Read(p) }

// reap waits for the child exactly once, recording its exit code. Both the
// reader (on EOF) and Close race to call it; the Once makes the second a no-op
// rather than a double os/exec Wait, which panics.
func (l *LogStream) reap() {
	l.once.Do(func() {
		_ = l.cmd.Wait()
		if l.cmd.ProcessState != nil {
			l.code = l.cmd.ProcessState.ExitCode()
		}
	})
}

// Wait returns the follow's exit code once it has ended.
func (l *LogStream) Wait() int {
	l.reap()
	return l.code
}

// Close stops the follow. Killing the child closes the last write end of the
// pipe, so a blocked Read unblocks with EOF.
func (l *LogStream) Close() error {
	if l.cmd.Process != nil {
		_ = l.cmd.Process.Kill()
	}
	_ = l.pr.Close()
	l.reap()
	return nil
}

// OpenContainerLogs authorizes, audits, and starts a live `docker logs --follow`.
// The caller owns the returned stream and must Close it.
func (b *Broker) OpenContainerLogs(ctx context.Context, req LogFollowRequest) (*LogStream, error) {
	ar := Request{Capability: CapContainerLogsFollow, Actor: req.Actor}

	// 1. Deny by default.
	if !b.pol.CapabilityEnabled(CapContainerLogsFollow) {
		b.record(audit.OutcomeDenied, ar, "capability disabled by policy")
		return nil, errx.Forbidden("capability_disabled", "Live log streaming is not enabled by policy.")
	}

	// 2. Validate the target. The pattern cannot start with "-", so a container
	// named like a flag is unrepresentable here as everywhere else.
	if err := capabilities.ValidateContainerRef(req.Container); err != nil {
		b.record(audit.OutcomeDenied, ar, "invalid container reference")
		return nil, err
	}

	// No ownership check: reading logs is not gated to managed containers, the
	// same as the polled logs capability. Mutation is the dangerous half, and none
	// happens here.

	tail := req.Tail
	if tail <= 0 || tail > maxFollowTail {
		tail = maxFollowTail
	}

	b.record(audit.OutcomeIntent, ar, req.Container)

	args := []string{"logs", "--follow", "--tail", strconv.Itoa(tail)}
	if req.Timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, req.Container)

	// stdout and stderr are joined into one pipe so the client sees the container's
	// output interleaved as the program wrote it. An *os.File target means os/exec
	// hands the fd to the child directly — no copier goroutine, so Wait blocks only
	// on the process, never on a drain.
	pr, pw, err := os.Pipe()
	if err != nil {
		b.record(audit.OutcomeFailure, ar, "pipe_failed")
		return nil, errx.Internal(err)
	}
	cmd := exec.Command(dockerBinary, args...)
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		b.log.Error("container logs follow failed", "err", err, "container", req.Container)
		b.record(audit.OutcomeFailure, ar, "start_failed")
		return nil, errx.Wrap(err, errx.KindInternal, "logs_follow_failed",
			"Could not start following the container's logs.")
	}
	// The parent's copy of the write end must be closed, or the read end never
	// sees EOF when the child exits (the parent would still hold a writer open).
	_ = pw.Close()

	b.record(audit.OutcomeSuccess, ar, req.Container)
	return &LogStream{cmd: cmd, pr: pr}, nil
}
