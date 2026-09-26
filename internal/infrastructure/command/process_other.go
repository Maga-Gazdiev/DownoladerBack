//go:build !unix

package command

import "os/exec"

func configure(cmd *exec.Cmd) {}
