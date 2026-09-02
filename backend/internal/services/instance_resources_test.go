package services

import (
	"testing"

	"github.com/homaserver/plati/internal/models"
)

// The templates on disk all write `cpu: 2` unquoted, and the template editor writes it
// through JSON.stringify(Number(...)), so templates.resources holds a JSON number.
// Decoding that into map[string]string used to fail, taking the whole map with it — which
// is why created instances carried neither limits.cpu nor limits.memory.
func TestResourcesFromJSON_NumericCPU(t *testing.T) {
	got := resourcesFromJSON(`{"cpu":2,"disk":"20GB","memory":"4GB"}`)

	want := map[string]string{"cpu": "2", "disk": "20GB", "memory": "4GB"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("resources[%q] = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestResourcesFromJSON_Forms(t *testing.T) {
	cases := []struct {
		name, raw, key, want string
	}{
		{"quoted cpu", `{"cpu":"2"}`, "cpu", "2"},
		{"float cpu", `{"cpu":2.0}`, "cpu", "2"},
		{"pinned set", `{"cpu":"0-3"}`, "cpu", "0-3"},
		{"percentage", `{"cpu":"50%"}`, "cpu", "50%"},
		// %v would render this as "1e+06", which Incus rejects as a limits value.
		{"large number", `{"memory":1000000}`, "memory", "1000000"},
		{"fractional", `{"cpu":2.5}`, "cpu", "2.5"},
		{"bool", `{"nested":true}`, "nested", "true"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resourcesFromJSON(c.raw)[c.key]; got != c.want {
				t.Errorf("resourcesFromJSON(%s)[%q] = %q, want %q", c.raw, c.key, got, c.want)
			}
		})
	}
}

func TestResourcesFromJSON_Degenerate(t *testing.T) {
	for _, raw := range []string{"", "{}", "not json", "[1,2]"} {
		if got := resourcesFromJSON(raw); len(got) != 0 {
			t.Errorf("resourcesFromJSON(%q) = %v, want empty", raw, got)
		}
	}
	// An explicit null is "unset", not the string "<nil>".
	if got, ok := resourcesFromJSON(`{"cpu":null}`)["cpu"]; ok {
		t.Errorf(`resources["cpu"] = %q for a null, want absent`, got)
	}
}

func TestInstanceResources_OverrideWins(t *testing.T) {
	tmpl := &models.Template{Resources: `{"cpu":2,"memory":"4GB","disk":"20GB"}`}
	inst := &models.Instance{LimitsCPU: "8", LimitsMemory: "16GB"}

	got := instanceResources(tmpl, inst)
	if got["cpu"] != "8" {
		t.Errorf("cpu = %q, want the override 8", got["cpu"])
	}
	if got["memory"] != "16GB" {
		t.Errorf("memory = %q, want the override 16GB", got["memory"])
	}
	// Disk has no override; it must still come through from the template.
	if got["disk"] != "20GB" {
		t.Errorf("disk = %q, want 20GB from the template", got["disk"])
	}
}

func TestInstanceResources_EmptyOverrideKeepsTemplate(t *testing.T) {
	tmpl := &models.Template{Resources: `{"cpu":2,"memory":"4GB"}`}

	for _, inst := range []*models.Instance{nil, {}, {LimitsCPU: "", LimitsMemory: ""}} {
		got := instanceResources(tmpl, inst)
		if got["cpu"] != "2" || got["memory"] != "4GB" {
			t.Errorf("instanceResources(%v) = %v, want the template's values", inst, got)
		}
	}
}

func TestInstanceResources_PartialOverride(t *testing.T) {
	tmpl := &models.Template{Resources: `{"cpu":2,"memory":"4GB"}`}
	got := instanceResources(tmpl, &models.Instance{LimitsCPU: "8"})

	if got["cpu"] != "8" {
		t.Errorf("cpu = %q, want 8", got["cpu"])
	}
	if got["memory"] != "4GB" {
		t.Errorf("memory = %q, want the template's 4GB when only cpu is overridden", got["memory"])
	}
}
