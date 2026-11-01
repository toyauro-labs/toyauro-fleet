// Package fleet is the peer mailbox: messages live in tmux named buffers on the
// recipient's tmux server, and SSH carries the same tmux operations to other
// devices. There is no daemon, and a stored message is not a read receipt.
package fleet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const commandTimeout = 15 * time.Second

var (
	paneRe   = regexp.MustCompile(`^%[0-9]+$`)
	serverRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	unsafeRe = regexp.MustCompile(`[^A-Za-z0-9_@%+=:,./-]`)
)

// Target is one tmux server. An empty Host is this machine.
type Target struct {
	Host, Server string
	// Port overrides the SSH port when nonzero; Termux listens on 8022.
	Port int
	// trustNewKey accepts a host key seen for the first time. Only the explicit
	// act of adding a device sets it; a changed key is still refused.
	trustNewKey bool
}

// HostLabel is how results name the host: "local" instead of an empty alias.
func (t Target) HostLabel() string {
	if t.Host == "" {
		return "local"
	}
	return t.Host
}

// ValidServer reports whether name is usable as a tmux -L socket name.
func ValidServer(name string) bool { return serverRe.MatchString(name) }

// quote makes s one word for a POSIX shell.
func quote(s string) string {
	if s != "" && !unsafeRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func shellJoin(argv []string) string {
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = quote(a)
	}
	return strings.Join(words, " ")
}

// sshArgs forces the options a user's SSH config must not change here:
// RemoteCommand or RequestTTY would otherwise attach a UI to a tool call.
func sshArgs(t Target, remote string) []string {
	args := []string{"-T", "-o", "RemoteCommand=none", "-o", "RequestTTY=no",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=8"}
	if t.trustNewKey {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	return append(append(args, portArgs(t.Port)...), "--", t.Host, remote)
}

func portArgs(port int) []string {
	if port == 0 {
		return nil
	}
	return []string{"-p", strconv.Itoa(port)}
}

// run executes argv on the target, reporting a non-zero exit as an error.
func (t Target) run(argv []string, stdin string) (string, error) {
	out, stderr, err := t.exec(argv, stdin)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", fmt.Errorf("tmux/SSH unavailable: %w", err)
		}
		if msg := strings.TrimSpace(stderr); msg != "" {
			return "", errors.New(msg)
		}
		return "", errors.New("tmux/SSH command failed")
	}
	return out, nil
}

func (t Target) exec(argv []string, stdin string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var c *exec.Cmd
	if t.Host == "" {
		c = exec.CommandContext(ctx, argv[0], argv[1:]...)
	} else {
		c = exec.CommandContext(ctx, "ssh", sshArgs(t, shellJoin(argv))...)
	}
	// A timed-out ssh can leave children holding the pipes open.
	c.WaitDelay = time.Second
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	c.Stdout, c.Stderr = &out, &errBuf
	err = c.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", commandTimeout)
	}
	return out.String(), errBuf.String(), err
}

func (t Target) tmux(args ...string) []string {
	return append([]string{"tmux", "-L", t.Server}, args...)
}

// Jump is a command that puts the user's terminal on a pane. Interactive
// commands own the terminal until they exit; a same-server switch returns at
// once and leaves the caller running in its own pane.
func Jump(p DevicePane) (c *exec.Cmd, interactive bool) {
	t := Target{Host: p.hostAlias(), Server: p.Server, Port: p.Port}
	id := p.Pane.Pane
	if t.Host == "" && CurrentServer() == t.Server {
		argv := t.tmux("switch-client", "-t", id)
		return exec.Command(argv[0], argv[1:]...), false
	}
	attach := t.tmux("select-window", "-t", id, ";", "select-pane", "-t", id, ";", "attach-session", "-t", id)
	if t.Host != "" {
		args := append([]string{"-t", "-o", "RemoteCommand=none"}, portArgs(t.Port)...)
		return exec.Command("ssh", append(args, "--", t.Host, shellJoin(attach))...), true
	}
	c = exec.Command(attach[0], attach[1:]...)
	// tmux refuses to nest while $TMUX names another server.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") {
			c.Env = append(c.Env, kv)
		}
	}
	return c, true
}

// CurrentServer is the tmux -L name of the server this process runs inside,
// or "" outside tmux.
func CurrentServer() string {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	if socket == "" {
		return ""
	}
	return filepath.Base(socket)
}
