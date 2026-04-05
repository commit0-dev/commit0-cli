//go:build windows

package surreal

import "os"

func interruptSignal() os.Signal {
	return os.Interrupt
}
