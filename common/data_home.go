package common

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// GetSidekickDataHome returns a directory path for storing user-specific
// sidekick data. If needed, it also creates the necessary directories for
// storing user-specific data according to the XDG spec.
func GetSidekickDataHome() (string, error) {
	sidekickDataDir := filepath.Join(xdg.DataHome, "sidekick")
	err := os.MkdirAll(sidekickDataDir, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create Sidekick data directory: %w", err)
	}
	return sidekickDataDir, nil
}