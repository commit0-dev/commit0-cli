package cmd

import (
	"os"
	"strings"
)

// useColor is true when stdout is a real terminal and NO_COLOR is not set.
var useColor = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.ToLower(os.Getenv("TERM")) == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}()

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiCyan    = "\033[36m"
	ansiYellow  = "\033[33m"
	ansiGray    = "\033[90m"
	ansiMagenta = "\033[35m"
	ansiGreen   = "\033[32m"
)

func colorize(codes, text string) string {
	if !useColor {
		return text
	}
	return codes + text + ansiReset
}

func bold(s string) string   { return colorize(ansiBold, s) }
func dim(s string) string    { return colorize(ansiDim, s) }
func gray(s string) string   { return colorize(ansiGray, s) }
func yellow(s string) string { return colorize(ansiYellow, s) }
func cyan(s string) string   { return colorize(ansiCyan, s) }
