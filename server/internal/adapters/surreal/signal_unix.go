//go:build !windows

package surreal

import (
	"os"
	"syscall"
)

func interruptSignal() os.Signal {
	return syscall.SIGTERM
}
