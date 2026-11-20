//go:build !linux

package main

import (
	"fmt"
	"os"

	"openpeats/internal/i18n"
)

// integrationCommand has nothing to install outside Linux yet.
func integrationCommand(string) int {
	fmt.Fprintln(os.Stderr, i18n.T("integration.unsupported"))
	return 1
}
