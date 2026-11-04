package claude

import "testing"

func TestWindowsPluginProjectPath(t *testing.T) {
	data := []byte(`{
		"installed": [{"id":"local@tools","scope":"project","projectPath":"C:\\Work\\Проект\\"}],
		"available": [{"pluginId":"local@tools","name":"local"}]
	}`)
	installed, offers, err := parsePlugins(data, `c:/work/проект`)
	if err != nil || len(installed) != 1 || len(offers) != 0 {
		t.Fatalf("same Windows project: installed=%v offers=%v err=%v", installed, offers, err)
	}
}
