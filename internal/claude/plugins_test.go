package claude

import (
	"reflect"
	"testing"
)

// The plugins of the project are the user's and its own; another project's own are not installed
// here, so they are on offer.
func TestParsePlugins(t *testing.T) {
	data := `{
	  "installed": [
	    {"id": "common@infra", "scope": "project", "enabled": false, "projectPath": "/elsewhere"},
	    {"id": "specs@infra", "scope": "user", "enabled": true},
	    {"id": "lint@infra", "scope": "local", "enabled": true, "projectPath": "/work/app/"}
	  ],
	  "available": [
	    {"pluginId": "common@infra", "name": "common", "description": "Shared skills", "marketplaceName": "infra"},
	    {"pluginId": "specs@infra", "name": "specs", "marketplaceName": "infra"},
	    {"pluginId": "lint@infra", "name": "lint", "marketplaceName": "infra"}
	  ]
	}`
	installed, offers, err := parsePlugins([]byte(data), "/work/app")
	if err != nil {
		t.Fatal(err)
	}
	want := []Plugin{{ID: "specs@infra", Scope: "user", Enabled: true}, {ID: "lint@infra", Scope: "local", Enabled: true, ProjectPath: "/work/app/"}}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("installed %+v", installed)
	}
	if want := []Offer{{ID: "common@infra", Name: "common", Description: "Shared skills"}}; !reflect.DeepEqual(offers, want) {
		t.Errorf("offers %+v", offers)
	}
	if _, _, err := parsePlugins([]byte("Installed plugins:"), "/work/app"); err == nil {
		t.Error("no error for text that is not JSON")
	}
}
