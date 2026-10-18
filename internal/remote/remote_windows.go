//go:build windows

package remote

import (
	"os"
	"os/exec"
	"path/filepath"
)

// sshBinaryPath resolves the ssh executable: the Windows OpenSSH
// location first, then PATH. Empty means not found.
var sshBinaryPath = func() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	bin := filepath.Join(root, "System32", "OpenSSH", "ssh.exe")
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
	return "no ssh executable in %SystemRoot%\\System32\\OpenSSH or PATH"
}
