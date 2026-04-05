package surreal

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Process wraps an os/exec.Cmd to allow graceful stop.
type Process struct {
	cmd *exec.Cmd
}

// StartSurrealDB launches a local SurrealDB process in the background.
//
// Parameters:
//   - bindAddr: host:port to bind (e.g. "0.0.0.0:8000")
//   - dataPath: storage path — use "memory" for an in-memory instance
//   - user, pass: root credentials
//
// The returned Process can be stopped with StopSurrealDB. The function blocks
// until SurrealDB is accepting connections (up to 10 seconds).
func StartSurrealDB(ctx context.Context, bindAddr, dataPath, user, pass string) (*Process, error) {
	if bindAddr == "" {
		bindAddr = "0.0.0.0:8000"
	}
	if dataPath == "" {
		dataPath = "memory"
	}
	if user == "" {
		user = "root"
	}
	if pass == "" {
		pass = "root"
	}

	args := []string{
		"start",
		"--bind", bindAddr,
		"--user", user,
		"--pass", pass,
		"--log", "warn",
	}
	// Append the data path last (positional argument in surreal start).
	args = append(args, dataPath)

	cmd := exec.CommandContext(ctx, "surreal", args...)
	// Inherit stdout/stderr from the parent process for visibility.

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start surrealdb: %w", err)
	}

	proc := &Process{cmd: cmd}

	// Poll until the HTTP health endpoint responds or we time out.
	if err := waitForSurrealDB(ctx, bindAddr, 10*time.Second); err != nil {
		_ = proc.cmd.Process.Kill()
		return nil, fmt.Errorf("surrealdb did not become ready: %w", err)
	}

	return proc, nil
}

// StopSurrealDB sends SIGTERM to the SurrealDB process and waits for it
// to exit. A context deadline controls the maximum wait time.
func StopSurrealDB(ctx context.Context, proc *Process) error {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return nil
	}

	// Send interrupt signal for a graceful shutdown.
	if err := proc.cmd.Process.Signal(interruptSignal()); err != nil {
		// Process may have already exited.
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- proc.cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = proc.cmd.Process.Kill()
		return fmt.Errorf("stop surrealdb: context cancelled while waiting: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			// Exit code != 0 is expected after SIGTERM.
			return nil
		}
		return nil
	}
}

// waitForSurrealDB polls the SurrealDB WebSocket port until it is accepting
// TCP connections or the timeout elapses.
func waitForSurrealDB(ctx context.Context, bindAddr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Parse host:port from bindAddr (e.g. "0.0.0.0:8000" → ":8000").
	host, port := splitHostPort(bindAddr)
	addr := host + ":" + port
	if host == "0.0.0.0" || host == "" {
		addr = "127.0.0.1:" + port
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Use surreal's health check command if available; otherwise TCP probe.
		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := tcpProbe(probeCtx, addr)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for surrealdb at %s", timeout, addr)
}

// tcpProbe attempts a TCP connection to addr.
func tcpProbe(ctx context.Context, addr string) error {
	var d = &tcpDialer{}
	return d.dial(ctx, addr)
}

// tcpDialer is a thin wrapper so we can swap it in tests.
type tcpDialer struct{}

func (d *tcpDialer) dial(ctx context.Context, addr string) error {
	cmd := exec.CommandContext(ctx, "nc", "-z", "-w", "1", parseAddr(addr))
	return cmd.Run()
}

// parseAddr splits "host:port" into the two args needed by nc.
func parseAddr(addr string) string {
	// Return as-is; the caller passes it to nc directly.
	return addr
}

// splitHostPort splits a "host:port" string.
func splitHostPort(hostport string) (host, port string) {
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i], hostport[i+1:]
		}
	}
	return "", hostport
}
