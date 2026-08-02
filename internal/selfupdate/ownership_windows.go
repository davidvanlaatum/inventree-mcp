//go:build windows

package selfupdate

import (
	"fmt"
)

func runtimeGOOS() string { return "windows" }

func validateOwnedRegular(string, bool) error {
	return fmt.Errorf("windows ownership validation: %w", ErrUnsupportedPlatform)
}

func validateOwnedDirectory(string) error {
	return fmt.Errorf("windows ownership validation: %w", ErrUnsupportedPlatform)
}

func validateTrustedAncestorChain(string) error {
	return fmt.Errorf("windows ownership validation: %w", ErrUnsupportedPlatform)
}
