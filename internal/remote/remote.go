// Package remote fetches the Quosparr status file from a development
// machine over the system ssh binary. Errors carry only the target host,
// the exit code, and a category; stdout/stderr content never leaves the
// runner so it cannot leak into logs. The package does not log.
package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config describes one remote target. Host is required and is passed to
// ssh verbatim, either "host" or "user@host". Port 0 means the ssh
// default and omits -p. An empty Dir means "~/.quosparr".
type Config struct {
	Host string
	Port int
	Dir  string
}

const (
	defaultDir         = "~/.quosparr"
	statusFile         = "status.json"
	connectTimeoutSecs = 8
	callTimeout        = 15 * time.Second
)

// Sentinel errors classifying ssh failures. The application layer maps
// these to user-facing text.
var (
	// ErrBinaryNotFound: no ssh executable could be resolved on this system.
	ErrBinaryNotFound = errors.New("ssh binary not found")
	// ErrConnectionFailed: ssh exited 255; connect or auth failed.
	ErrConnectionFailed = errors.New("ssh connection failed")
	// ErrFileNotFound: ssh exited 1; the remote file is missing.
	ErrFileNotFound = errors.New("remote file not found")
)

// Client fetches status over ssh. The zero value is not usable; use New.
type Client struct {
	cfg    Config
	runner Runner
}

// Option customises a Client.
type Option func(*Client)

// WithRunner injects the command executor. Tests supply a fake; the
// default resolves and runs the real ssh binary for the platform.
func WithRunner(r Runner) Option {
	return func(c *Client) { c.runner = r }
}

// New returns a Client for cfg. The host is not validated here so the
// caller can surface its own message.
func New(cfg Config, opts ...Option) *Client {
	c := &Client{cfg: cfg}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) runnerOrDefault() Runner {
	if c.runner != nil {
		return c.runner
	}
	return newSSHRunner()
}

func (c *Client) dir() string {
	if c.cfg.Dir == "" {
		return defaultDir
	}
	return c.cfg.Dir
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, callTimeout)
}

// sshArgs builds the ssh argument list. remoteCmd is the already-quoted
// remote command string.
func (c *Client) sshArgs(remoteCmd string) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", connectTimeoutSecs),
	}
	if c.cfg.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", c.cfg.Port))
	}
	args = append(args, c.cfg.Host, remoteCmd)
	return args
}

// run executes the ssh invocation and classifies failures.
func (c *Client) run(ctx context.Context, remoteCmd string) ([]byte, error) {
	bin := sshBinaryPath()
	if bin == "" {
		return nil, fmt.Errorf("%w: %s", ErrBinaryNotFound, sshBinaryHint())
	}
	parent, cancel := c.withTimeout(ctx)
	defer cancel()
	stdout, exitCode, err := c.runnerOrDefault().Run(parent, bin, c.sshArgs(remoteCmd), nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ssh to %s: %w", c.cfg.Host, ctx.Err())
		}
		return nil, fmt.Errorf("ssh to %s: %w", c.cfg.Host, err)
	}
	switch exitCode {
	case 0:
		return stdout, nil
	case 1:
		return nil, fmt.Errorf("%w: %s: exit 1", ErrFileNotFound, c.cfg.Host)
	case 255:
		return nil, fmt.Errorf("%w: %s: exit 255", ErrConnectionFailed, c.cfg.Host)
	default:
		return nil, fmt.Errorf("ssh to %s: unexpected exit %d", c.cfg.Host, exitCode)
	}
}

// FetchStatus runs `ssh ... <host> cat <dir>/status.json` and returns the
// raw file bytes. The remote path keeps a leading tilde-prefix unquoted so
// the remote shell expands it; everything after it is single-quoted so
// spaces and quotes in Dir cannot break the command.
func (c *Client) FetchStatus(ctx context.Context) ([]byte, error) {
	return c.run(ctx, "cat "+remotePath(c.dir()+"/"+statusFile))
}

// TestConnection runs the same ssh invocation with the remote command
// `true`, verifying only that the binary resolves and the connection and
// authentication work.
func (c *Client) TestConnection(ctx context.Context) error {
	_, err := c.run(ctx, "true")
	return err
}

// remotePath quotes a remote path for the remote shell. A path made only of
// plain characters goes through bare (the common ~/.quosparr case, where a
// quoted tilde would never expand). Any other path splits off an unquoted
// tilde-prefix ("~/" or "~user/") and single-quotes the remainder.
func remotePath(path string) string {
	if isPlainRemotePath(path) {
		return path
	}
	if i := strings.IndexByte(path, '/'); i > 0 && path[0] == '~' {
		return path[:i+1] + quoteRemote(path[i+1:])
	}
	return quoteRemote(path)
}

// isPlainRemotePath reports whether path needs no quoting at all: no
// whitespace, quotes, globs, or shell operators, and no leading dash that
// could read as a cat option.
func isPlainRemotePath(path string) bool {
	if path == "" || path[0] == '-' {
		return false
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '/' || c == '_' || c == '-' || c == '~' || c == '=' || c == ':' || c == '%' || c == '@' || c == '+':
		default:
			return false
		}
	}
	return true
}

// quoteRemote single-quotes s for the remote shell, escaping embedded
// single quotes as '\”.
func quoteRemote(s string) string {
	const q = '\''
	out := make([]byte, 0, len(s)+2)
	out = append(out, q)
	for i := 0; i < len(s); i++ {
		if s[i] == q {
			out = append(out, q, '\\', q, q)
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, q)
	return string(out)
}
