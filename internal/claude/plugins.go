package claude

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// claude -p cannot run /plugins: claudit manages the plugins through `claude plugin`.

// Plugin is a plugin installed for a project: for the user, or by the project itself.
type Plugin struct {
	ID          string `json:"id"`      // name@marketplace
	Scope       string `json:"scope"`   // "user", "project" or "local"
	Enabled     bool   `json:"enabled"` // in the project
	ProjectPath string `json:"projectPath"`
}

// Offer is a plugin a marketplace offers.
type Offer struct {
	ID          string `json:"pluginId"` // name@marketplace
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Plugins lists the plugins installed for the project in dir and the others the marketplaces offer.
func Plugins(dir string) ([]Plugin, []Offer, error) {
	out, err := PluginCommand(dir, "list", "--json", "--available")
	if err != nil {
		return nil, nil, err
	}
	return parsePlugins([]byte(out), dir)
}

// parsePlugins reads `claude plugin list --json --available` run in dir. The project scopes of
// other projects are theirs alone: they are neither installed here nor kept from the offers.
func parsePlugins(data []byte, dir string) ([]Plugin, []Offer, error) {
	var list struct {
		Installed []Plugin `json:"installed"`
		Available []Offer  `json:"available"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, nil, fmt.Errorf("claude plugin list: %w", err)
	}
	here := map[string]bool{}
	dir = filepath.Clean(dir)
	installed := slices.DeleteFunc(list.Installed, func(p Plugin) bool {
		path := filepath.Clean(p.ProjectPath)
		same := path == dir || runtime.GOOS == "windows" && strings.EqualFold(path, dir)
		mine := p.Scope == "user" || p.ProjectPath != "" && same
		here[p.ID] = here[p.ID] || mine
		return !mine
	})
	offers := slices.DeleteFunc(list.Available, func(o Offer) bool { return here[o.ID] })
	return installed, offers, nil
}

// PluginCommand runs `claude plugin args...` in dir and returns what it printed. When it fails,
// the error is what it said.
func PluginCommand(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), claudeProgram, append([]string{"plugin"}, args...)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cleanup, err := startProcess(cmd)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if err := cmd.Wait(); err != nil {
		if said := strings.TrimSpace(cmp.Or(stderr.String(), stdout.String())); said != "" {
			return "", errors.New(said)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
