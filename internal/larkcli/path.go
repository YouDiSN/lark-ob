package larkcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolvePath prefers an explicitly configured CLI and the user's current
// installation over an older system-wide binary inherited through systemd's PATH.
func ResolvePath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("LARK_CLI_PATH")); configured != "" {
		if executable(configured) {
			return configured, nil
		}
		return "", fmt.Errorf("LARK_CLI_PATH 不可执行: %s", configured)
	}
	if home, err := os.UserHomeDir(); err == nil {
		local := filepath.Join(home, ".local", "bin", "lark-cli")
		if executable(local) {
			return local, nil
		}
	}
	path, err := exec.LookPath("lark-cli")
	if err != nil {
		return "", fmt.Errorf("lark-cli 未安装: %w", err)
	}
	return path, nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
