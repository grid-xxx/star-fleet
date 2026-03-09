package agent

import (
	"io"
	"os/exec"

	"github.com/creack/pty"
)

// runWithPTY starts cmd inside a pseudo-terminal and copies all PTY output to w
// (when non-nil). It waits for the command to finish and returns any error.
func runWithPTY(cmd *exec.Cmd, w io.Writer) error {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	if w != nil {
		// Copy PTY output until EOF (command exits).
		// The read side returns io.EOF once the child process closes,
		// so we intentionally ignore that error.
		io.Copy(w, ptmx)
	}

	return cmd.Wait()
}
