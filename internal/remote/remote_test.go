package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records invocations and replays canned results.
type fakeRunner struct {
	calls    []fakeCall
	stdout   []byte
	exitCode int
	err      error
	runErr   error // returned as err from Run, takes precedence
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, int, error) {
	f.calls = append(f.calls, fakeCall{name, append([]string(nil), args...)})
	if f.runErr != nil {
		return nil, 0, f.runErr
	}
	return f.stdout, f.exitCode, nil
}

func newClient(t *testing.T, cfg Config, f *fakeRunner) *Client {
	t.Helper()
	return New(cfg, WithRunner(f))
}

func TestFetchStatusSuccess(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`{"ok":true}`)}
	c := newClient(t, Config{Host: "dev"}, f)
	got, err := c.FetchStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("stdout not passed through: %q", got)
	}
}

func TestFetchStatusExit1FileNotFound(t *testing.T) {
	f := &fakeRunner{exitCode: 1}
	c := newClient(t, Config{Host: "dev"}, f)
	_, err := c.FetchStatus(context.Background())
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("want ErrFileNotFound, got %v", err)
	}
}

func TestFetchStatusExit255ConnectionFailed(t *testing.T) {
	f := &fakeRunner{exitCode: 255}
	c := newClient(t, Config{Host: "dev"}, f)
	_, err := c.FetchStatus(context.Background())
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("want ErrConnectionFailed, got %v", err)
	}
}

func TestBinaryNotFoundSentinel(t *testing.T) {
	orig := sshBinaryPath
	sshBinaryPath = func() string { return "" }
	t.Cleanup(func() { sshBinaryPath = orig })
	c := New(Config{Host: "dev"})
	if _, err := c.FetchStatus(context.Background()); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound, got %v", err)
	}
	if err := c.TestConnection(context.Background()); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound from TestConnection, got %v", err)
	}
}

func TestArgsAssembly(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "default port and dir",
			cfg:  Config{Host: "dev"},
			want: "-o BatchMode=yes -o ConnectTimeout=8 dev cat ~/.quosparr/status.json",
		},
		{
			name: "explicit port",
			cfg:  Config{Host: "dev", Port: 2222},
			want: "-o BatchMode=yes -o ConnectTimeout=8 -p 2222 dev cat ~/.quosparr/status.json",
		},
		{
			name: "user@host verbatim",
			cfg:  Config{Host: "me@dev.example", Dir: "/srv/data"},
			want: "-o BatchMode=yes -o ConnectTimeout=8 me@dev.example cat /srv/data/status.json",
		},
		{
			name: "dir with spaces",
			cfg:  Config{Host: "dev", Dir: "/opt/my dir"},
			want: "-o BatchMode=yes -o ConnectTimeout=8 dev cat '/opt/my dir/status.json'",
		},
		{
			name: "dir with single quote",
			cfg:  Config{Host: "dev", Dir: "/opt/it's"},
			want: `-o BatchMode=yes -o ConnectTimeout=8 dev cat '/opt/it'\''s/status.json'`,
		},
		{
			name: "tilde dir with spaces stays expandable",
			cfg:  Config{Host: "dev", Dir: "~/.my dir"},
			want: `-o BatchMode=yes -o ConnectTimeout=8 dev cat ~/'.my dir/status.json'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{stdout: []byte("{}")}
			c := newClient(t, tc.cfg, f)
			if _, err := c.FetchStatus(context.Background()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("want 1 call, got %d", len(f.calls))
			}
			got := strings.Join(f.calls[0].args, " ")
			if got != tc.want {
				t.Fatalf("args mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestTestConnectionArgsAndSuccess(t *testing.T) {
	f := &fakeRunner{}
	c := newClient(t, Config{Host: "dev", Port: 2202}, f)
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(f.calls[0].args, " ")
	want := "-o BatchMode=yes -o ConnectTimeout=8 -p 2202 dev true"
	if got != want {
		t.Fatalf("args mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestContextCancelPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeRunner{runErr: context.Canceled}
	c := newClient(t, Config{Host: "dev"}, f)
	cancel()
	_, err := c.FetchStatus(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestErrorCarriesHostNotOutput(t *testing.T) {
	f := &fakeRunner{exitCode: 255, stdout: []byte("permission denied detail")}
	c := newClient(t, Config{Host: "me@dev"}, f)
	_, err := c.FetchStatus(context.Background())
	if !strings.Contains(err.Error(), "me@dev") {
		t.Fatalf("error should carry host: %v", err)
	}
	if strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error must not carry stdout content: %v", err)
	}
}
