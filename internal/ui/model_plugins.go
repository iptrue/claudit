package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/claude"
)

// claude -p cannot run /plugins: claudit manages the plugins through `claude plugin`, in commands
// off the UI's way, and has claude reload them after a change.

// The CLI the manager runs; tests stand in for it.
var (
	pluginsList    = claude.Plugins
	pluginsCommand = claude.PluginCommand
)

// PluginsPopup is /plugins: the plugins installed for the project, each on or off, and the ones the
// marketplaces offer besides, found by what their name or description has.
type PluginsPopup struct {
	Discover  bool // the tab of the offers; else the one of the plugins installed
	Installed []claude.Plugin
	Offers    []claude.Offer
	Filter    Input  // what the offers shown have
	Shown     []int  // the offers Filter finds
	Cursor    int    // the highlighted row of the tab
	Loading   bool   // the lists are being read
	Busy      string // the plugin a change runs on, one at a time; "" for none
	Doing     string // what the change does: "installing", …
	Confirm   string // the plugin a second u uninstalls
}

func (*PluginsPopup) isPopup() {}

// rows is how many rows the tab lists.
func (p *PluginsPopup) rows() int {
	if p.Discover {
		return len(p.Shown)
	}
	return len(p.Installed)
}

// find shows the offers whose id or description has Filter, in any case.
func (p *PluginsPopup) find() {
	q := strings.ToLower(strings.TrimSpace(string(p.Filter.Text)))
	p.Shown = p.Shown[:0]
	for i, o := range p.Offers {
		if strings.Contains(strings.ToLower(o.ID), q) || strings.Contains(strings.ToLower(o.Description), q) {
			p.Shown = append(p.Shown, i)
		}
	}
	p.Cursor = min(p.Cursor, max(0, p.rows()-1))
}

// pluginsMsg brings the plugins of the project and the offers.
type pluginsMsg struct {
	installed []claude.Plugin
	offers    []claude.Offer
	err       error
}

// pluginChangedMsg brings what `claude plugin` said to a change.
type pluginChangedMsg struct {
	out string
	err error
}

// openPlugins runs /plugins: the manager comes up and reads the lists.
func (m *Model) openPlugins() tea.Cmd {
	if m.Status.Replay { // the plugins are the user's own, not the recording's
		m.toast(ToastInfo, -1, "replay: /plugins is not available")
		return nil
	}
	m.Popup = &PluginsPopup{Loading: true}
	return m.readPlugins()
}

func (m *Model) readPlugins() tea.Cmd {
	root := m.Sim.State().Root
	return func() tea.Msg {
		installed, offers, err := pluginsList(root)
		return pluginsMsg{installed: installed, offers: offers, err: err}
	}
}

// pluginsRead fills the manager with the lists, unless it was closed meanwhile.
func (m *Model) pluginsRead(msg pluginsMsg) {
	p, ok := m.Popup.(*PluginsPopup)
	switch {
	case !ok:
	case msg.err != nil:
		m.toast(ToastErr, -1, "/plugins: %v", msg.err)
		p.Busy, p.Doing = "", ""
		if p.Loading { // nothing to show
			m.closeResume()
		}
	default:
		p.Installed, p.Offers, p.Loading, p.Busy, p.Doing = msg.installed, msg.offers, false, "", ""
		p.find()
	}
}

func (m *Model) pluginsKey(p *PluginsPopup, k tea.KeyPressMsg) tea.Cmd {
	key, confirm := k.String(), p.Confirm
	p.Confirm = ""
	idle := !p.Loading && p.Busy == ""
	switch {
	case key == "esc":
		m.closeResume()
	case key == "tab":
		p.Discover, p.Cursor = !p.Discover, 0
	case key == "up":
		p.Cursor = max(p.Cursor-1, 0)
	case key == "down":
		p.Cursor = min(p.Cursor+1, max(0, p.rows()-1))
	case key == "pgup":
		p.Cursor = max(p.Cursor-10, 0)
	case key == "pgdown":
		p.Cursor = min(p.Cursor+10, max(0, p.rows()-1))
	case p.Discover && key == "enter":
		if idle && len(p.Shown) > 0 {
			id := p.Offers[p.Shown[p.Cursor]].ID
			return m.changePlugin(p, id, "installing", "install", id)
		}
	case p.Discover:
		was := string(p.Filter.Text)
		if p.Filter.edit(k) && string(p.Filter.Text) != was {
			p.Cursor = 0
			p.find()
		}
	case !idle || len(p.Installed) == 0: // one change at a time
	case key == "enter" || key == "space":
		pl := p.Installed[p.Cursor]
		if pl.Enabled {
			return m.changePlugin(p, pl.ID, "turning off", "disable", pl.ID, "--scope", pl.Scope)
		}
		return m.changePlugin(p, pl.ID, "turning on", "enable", pl.ID, "--scope", pl.Scope)
	case key == "u" && confirm == p.Installed[p.Cursor].ID:
		pl := p.Installed[p.Cursor]
		return m.changePlugin(p, pl.ID, "uninstalling", "uninstall", pl.ID, "--scope", pl.Scope)
	case key == "u":
		p.Confirm = p.Installed[p.Cursor].ID
	}
	return nil
}

// changePlugin runs `claude plugin args...` on the plugin id; doing says what it does meanwhile.
func (m *Model) changePlugin(p *PluginsPopup, id, doing string, args ...string) tea.Cmd {
	p.Busy, p.Doing = id, doing
	root := m.Sim.State().Root
	return func() tea.Msg {
		out, err := pluginsCommand(root, args...)
		return pluginChangedMsg{out: out, err: err}
	}
}

// pluginChanged puts what `claude plugin` did in the chat, has claude reload the plugins and the
// manager read the lists again; what it refused is a toast.
func (m *Model) pluginChanged(msg pluginChangedMsg) tea.Cmd {
	p, open := m.Popup.(*PluginsPopup)
	if msg.err != nil {
		m.toast(ToastErr, -1, "%v", msg.err)
		if open {
			p.Busy, p.Doing = "", ""
		}
		return nil
	}
	m.local(msg.out)
	m.reloadPlugins()
	if open {
		return m.readPlugins() // the change shows once the lists say it
	}
	return nil
}

// reloadPlugins has claude load the plugins again, after its turn when it works.
func (m *Model) reloadPlugins() {
	const reload = "/reload-plugins"
	switch {
	case m.Status.State == "exited" || !m.known["reload-plugins"]: // a claude without it would take it for a prompt
	case m.Status.Busy:
		if !slices.Contains(m.pending, reload) {
			m.pending = append(m.pending, reload)
		}
	default:
		m.send(m.message(reload))
		m.Status.State, m.Status.Busy, m.cmdTurn = "thinking", true, true
	}
}
