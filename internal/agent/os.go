package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// detectOS reads /etc/os-release or the specified etc directory
// and extracts the operating system distribution and version.
// Falls back to "Debian 13 / Linux" if unreadable or unparseable.
func detectOS(etcDir string) string {
	if etcDir == "" {
		etcDir = "/etc"
	}
	path := filepath.Clean(filepath.Join(etcDir, "os-release"))
	// #nosec G304 -- reading host release information
	f, err := os.Open(path)
	if err != nil {
		if etcDir == "/etc" {
			// #nosec G304 -- reading host release information fallback
			f, err = os.Open("/usr/lib/os-release")
		}
		if err != nil {
			return "Debian 13 / Linux"
		}
	}
	defer f.Close()

	var prettyName, name, versionID string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch k {
		case "PRETTY_NAME":
			prettyName = v
		case "NAME":
			name = v
		case "VERSION_ID":
			versionID = v
		}
	}
	if prettyName != "" {
		return prettyName
	}
	if name != "" {
		if versionID != "" {
			return name + " " + versionID
		}
		return name
	}
	return "Debian 13 / Linux"
}
