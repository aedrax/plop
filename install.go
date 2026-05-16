package main

import (
	"fmt"
)

// InstallOptions defines custom installation properties supplied via CLI flags or upgrades
type InstallOptions struct {
	ForcedName          string
	ForcedVersion       string
	ForcedSource        string
	ForcedBinaryName    string
	ForcedInstallScript string
}

// InstallApp carries out the absolute installation lifecycle
func InstallApp(sourcePathOrURL string, options InstallOptions) error {
	return fmt.Errorf("installation logic not implemented")
}
