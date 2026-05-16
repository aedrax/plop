package main

import (
	"fmt"
	"os"
)

// ANSI terminal colors
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorGray   = "\033[90m"
)

var colorsEnabled = true

// InitColors initializes the colorsEnabled state based on standard environment checks
func InitColors() {
	noColorEnv := os.Getenv("NO_COLOR") != ""
	cliColorForce := os.Getenv("CLICOLOR_FORCE") != ""

	// Check if stdout is a character device (TTY)
	stdoutIsTTY := true
	if fi, err := os.Stdout.Stat(); err == nil {
		stdoutIsTTY = (fi.Mode() & os.ModeCharDevice) != 0
	}

	// Disable colors if NO_COLOR is set or if output is piped/redirected (and not forced)
	if noColorEnv || (!stdoutIsTTY && !cliColorForce) {
		colorsEnabled = false
	} else {
		colorsEnabled = true
	}
}

// color formats text with the given ANSI style prefix and appends ColorReset,
// unless colors are disabled, in which case it returns the text as-is.
func color(style, text string) string {
	if !colorsEnabled || text == "" {
		return text
	}
	return style + text + ColorReset
}

// PrintSuccess prints a formatted success message
func PrintSuccess(format string, a ...interface{}) {
	fmt.Printf(color(ColorGreen, format)+"\n", a...)
}

// PrintInfo prints a formatted info message
func PrintInfo(format string, a ...interface{}) {
	fmt.Printf(color(ColorCyan, format)+"\n", a...)
}

// PrintWarning prints a formatted warning message
func PrintWarning(format string, a ...interface{}) {
	fmt.Printf(color(ColorYellow, format)+"\n", a...)
}

// PrintError prints a formatted error message
func PrintError(format string, a ...interface{}) {
	fmt.Printf(color(ColorRed+ColorBold, format)+"\n", a...)
}
