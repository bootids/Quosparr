//go:build darwin || linux

package remote

import "os/exec"

// sshBinaryPath resolves the ssh executable: the standard location
// first, then PATH. Empty means not found.
var sshBinaryPath = func() string {
	const bin = "/usr/bin/ssh"
	if path, err := exec.LookPath(bin); err == nil {
		return path
	}
	if path, err := exec.LookPath("ssh"); err == nil {
		return path
	}
	return ""
}

// sshBinaryHint is the platform-readable hint for ErrBinaryNotFound. It
// carries no stderr content.
func sshBinaryHint() string {
	return "no ssh executable in /usr/bin or PATH"
}
