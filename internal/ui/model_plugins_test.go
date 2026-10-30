package ui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/claude"
	"github.com/pixelbender/claudit/internal/protocol"
)

// modelPluginsNew is a live model whose `claude plugin` is a stand-in that keeps its plugins and
// records the commands run. An install of docs@web fails, as one that runs a command does.
func modelPluginsNew(t *testing.T) (*Model, *[]protocol.Outbound, *[][]string) {
	t.Helper()
	m, sent := modelNew(t, false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	modelEvents(m, protocol.Commands{List: []protocol.Command{{Name: "reload-plugins"}}})
	installed := []claude.Plugin{{ID: "specs@infra", Scope: "user", Enabled: true}, {ID: "lint@infra", Scope: "project"}}
	offers := []claude.Offer{{ID: "common@infra", Description: "Shared skills"}, {ID: "docs@web", Description: "Read the docs"}}
	ran := new([][]string)
	list, command := pluginsList, pluginsCommand
	t.Cleanup(func() { pluginsList, pluginsCommand = list, command })
	pluginsList = func(string) ([]claude.Plugin, []claude.Offer, error) {
		return slices.Clone(installed), slices.Clone(offers), nil
	}
	pluginsCommand = func(_ string, args ...string) (string, error) {
		*ran = append(*ran, args)
		at := slices.IndexFunc(installed, func(p claude.Plugin) bool { return p.ID == args[1] })
		switch args[0] {
		case "install":
			if args[1] == "docs@web" {
				return "", errors.New("✘ docs@web runs a command: pass -y")
			}
			installed = append(installed, claude.Plugin{ID: args[1], Scope: "user", Enabled: true})
			offers = slices.DeleteFunc(offers, func(o claude.Offer) bool { return o.ID == args[1] })
		case "enable", "disable":
			installed[at].Enabled = args[0] == "enable"
		case "uninstall":
			installed = slices.Delete(installed, at, at+1)
		}
		return "✔ " + args[0] + " " + args[1], nil
	}
	return m, sent, ran
}

// /plugins lists the plugins installed; space turns one off, u twice uninstalls one. What the CLI
// says goes in the chat and claude reloads the plugins, after its turn when it works.
func TestModelPluginsInstalled(t *testing.T) {
	m, sent, ran := modelPluginsNew(t)
	modelResumeRun(m, modelLocalSay(m, "/plugins"))
	p, ok := m.Popup.(*PluginsPopup)
	if !ok || p.Loading || len(p.Installed) != 2 || len(*sent) != 0 {
		t.Fatalf("popup %+v, sent %#v", m.Popup, *sent)
	}
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "● specs@infra  user") || !strings.Contains(out, "○ lint@infra   project") {
		t.Errorf("the plugins are not listed:\n%s", out)
	}

	_, cmd := m.Update(inputKey("space"))
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "specs@infra  turning off…") {
		t.Errorf("no word of the change:\n%s", out)
	}
	modelResumeRun(m, cmd)
	if want := [][]string{{"disable", "specs@infra", "--scope", "user"}}; !reflect.DeepEqual(*ran, want) {
		t.Errorf("ran %q", *ran)
	}
	if p.Installed[0].Enabled || p.Busy != "" || m.Chat[len(m.Chat)-1].Text != "✔ disable specs@infra" {
		t.Errorf("after the change: popup %+v, chat %+v", p, m.Chat)
	}
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "/reload-plugins" {
		t.Errorf("sent %#v", *sent)
	}

	modelPress(m, inputKey("down"), inputKey("u"))
	if len(*ran) != 1 || !strings.Contains(ansi.Strip(m.View().Content), "u again uninstalls lint@infra") {
		t.Fatalf("the first u: ran %q", *ran)
	}
	_, cmd = m.Update(inputKey("u"))
	modelResumeRun(m, cmd)
	if len(*ran) != 2 || len(p.Installed) != 1 || len(*sent) != 1 || !slices.Equal(m.pending, []string{"/reload-plugins"}) {
		t.Errorf("uninstalled while claude reloads: ran %q, installed %+v, sent %d, pending %q", *ran, p.Installed, len(*sent), m.pending)
	}
	modelPress(m, inputKey("esc"))
	if m.Popup != nil {
		t.Errorf("esc left %+v", m.Popup)
	}
}

// The offers are found by what their name or description has; enter installs one. One the CLI
// refuses is said in the chat, and nothing is reloaded.
func TestModelPluginsDiscover(t *testing.T) {
	m, sent, ran := modelPluginsNew(t)
	modelResumeRun(m, modelLocalSay(m, "/plugin"))
	p := m.Popup.(*PluginsPopup)
	modelPress(m, inputKey("tab"))
	modelPress(m, inputTyped("DOCS")...)
	if !p.Discover || !slices.Equal(p.Shown, []int{1}) {
		t.Fatalf("found %v", p.Shown)
	}
	_, cmd := m.Update(inputKey("enter"))
	modelResumeRun(m, cmd)
	if !modelResumeSaid(m, "docs@web runs a command") || p.Busy != "" || len(*sent) != 0 {
		t.Errorf("refused: chat %+v, busy %q, sent %#v", m.Chat, p.Busy, *sent)
	}

	modelPress(m, inputKey("ctrl+u"))
	_, cmd = m.Update(inputKey("enter"))
	modelResumeRun(m, cmd)
	if len(*ran) != 2 || (*ran)[1][1] != "common@infra" || len(p.Offers) != 1 || len(p.Installed) != 3 || len(*sent) != 1 {
		t.Errorf("install: ran %q, offers %+v, installed %+v, sent %#v", *ran, p.Offers, p.Installed, *sent)
	}
}

// A replay has no plugins of its own to manage.
func TestModelPluginsReplay(t *testing.T) {
	m, _ := modelNew(t, true)
	if cmd := modelLocalSay(m, "/plugins"); cmd != nil || m.Popup != nil || !modelResumeSaid(m, "/plugins is not available") {
		t.Errorf("replay: popup %+v, chat %+v", m.Popup, m.Chat)
	}
}
