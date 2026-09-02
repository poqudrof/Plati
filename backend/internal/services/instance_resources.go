package services

import (
	"fmt"
	"regexp"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

// ResourceUpdate carries the admin-set limits for one instance. An empty field clears the
// override, reverting that limit to the template's value — it does not mean "unlimited".
type ResourceUpdate struct {
	LimitsCPU    string `json:"limits_cpu"`
	LimitsMemory string `json:"limits_memory"`
}

// InstanceResources is what the Resources tab renders: where each limit comes from, so
// the UI can say "from the template" or "set by an administrator" rather than showing a
// bare number nobody can explain.
type InstanceResources struct {
	// TemplateCPU and TemplateMemory are the template's own values.
	TemplateCPU    string `json:"template_cpu"`
	TemplateMemory string `json:"template_memory"`
	// OverrideCPU and OverrideMemory are the per-instance values, empty when inherited.
	OverrideCPU    string `json:"override_cpu"`
	OverrideMemory string `json:"override_memory"`
	// EffectiveCPU and EffectiveMemory are what Plati will hand Incus.
	EffectiveCPU    string `json:"effective_cpu"`
	EffectiveMemory string `json:"effective_memory"`
	// TemplateDisk is read-only: Plati has no volume resize path yet, and the
	// container's root filesystem is not size-limited at all.
	TemplateDisk string `json:"template_disk"`
	// Editable reports whether the caller may change these, i.e. whether they are an
	// admin. The write route is admin-gated regardless; this only drives the UI.
	Editable bool `json:"editable"`
	// Applied reports whether the last write reached Incus. False means the value is
	// stored and will take effect at the next start or rebuild.
	Applied bool   `json:"applied"`
	Warning string `json:"warning,omitempty"`
}

var (
	// A CPU count ("2"), a pinned set ("0-3", "0,2,4") or a percentage ("50%").
	cpuLimitRe = regexp.MustCompile(`^([0-9]+|[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*|[0-9]+%)$`)
	// A size with a unit, or a percentage of the host's memory.
	memoryLimitRe = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?(B|kB|MB|GB|TB|KiB|MiB|GiB|TiB)|[0-9]+%)$`)
)

// validateResourceUpdate rejects malformed limits before they are stored. Incus accepts
// them at UpdateInstance time and only fails much later, when the container next starts —
// by which point the admin has long since navigated away.
func validateResourceUpdate(req ResourceUpdate) error {
	if req.LimitsCPU != "" && !cpuLimitRe.MatchString(req.LimitsCPU) {
		return fmt.Errorf("invalid cpu limit %q: expected a count (2), a pinned set (0-3) or a percentage (50%%)", req.LimitsCPU)
	}
	if req.LimitsMemory != "" && !memoryLimitRe.MatchString(req.LimitsMemory) {
		return fmt.Errorf("invalid memory limit %q: expected a size with a unit (4GB, 512MiB) or a percentage", req.LimitsMemory)
	}
	return nil
}

// GetResources reports the resource limits of an instance the actor may reach.
func (s *InstanceService) GetResources(id int64, actor auth.Actor) (*InstanceResources, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	tmpl, err := queries.GetTemplate(s.db, inst.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}
	return s.resourcesFor(tmpl, inst, actor.Admin), nil
}

func (s *InstanceService) resourcesFor(tmpl *models.Template, inst *models.Instance, editable bool) *InstanceResources {
	tmplRes := resourcesFromJSON(tmpl.Resources)
	effective := instanceResources(tmpl, inst)
	return &InstanceResources{
		TemplateCPU:     tmplRes["cpu"],
		TemplateMemory:  tmplRes["memory"],
		TemplateDisk:    tmplRes["disk"],
		OverrideCPU:     inst.LimitsCPU,
		OverrideMemory:  inst.LimitsMemory,
		EffectiveCPU:    effective["cpu"],
		EffectiveMemory: effective["memory"],
		Editable:        editable,
		Applied:         true,
	}
}

// UpdateResources stores the per-instance limits and pushes them to Incus.
//
// The DB write comes first on purpose: if Incus is unreachable the stored value still
// wins at the next rebuild, which is the direction that converges. That ordering is the
// whole point of the feature — UpdateIncusConfig wrote only to Incus, so every limit an
// admin set evaporated the next time the owner rebuilt.
//
// An Incus failure comes back as a warning rather than an error, so the UI can honestly
// say "saved, applies at the next start" instead of claiming the write failed.
func (s *InstanceService) UpdateResources(id int64, actor auth.Actor, req ResourceUpdate) (*InstanceResources, error) {
	if err := validateResourceUpdate(req); err != nil {
		return nil, err
	}
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	tmpl, err := queries.GetTemplate(s.db, inst.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	if err := queries.UpdateInstanceResources(s.db, id, req.LimitsCPU, req.LimitsMemory); err != nil {
		return nil, fmt.Errorf("save resource limits: %w", err)
	}
	inst.LimitsCPU = req.LimitsCPU
	inst.LimitsMemory = req.LimitsMemory

	out := s.resourcesFor(tmpl, inst, actor.Admin)

	// Resolve through instanceResources rather than sending req as-is: UpdateInstanceConfig
	// treats an empty value as "delete this key", so clearing an override would leave the
	// container with no limit at all instead of the template's.
	if err := s.applyResourcesToIncus(inst, tmpl); err != nil {
		out.Applied = false
		out.Warning = fmt.Sprintf("saved, but Incus did not accept it yet: %v", err)
	}
	return out, nil
}

// applyResourcesToIncus pushes the instance's effective CPU and memory limits live.
func (s *InstanceService) applyResourcesToIncus(inst *models.Instance, tmpl *models.Template) error {
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return fmt.Errorf("get incus client: %w", err)
	}
	effective := instanceResources(tmpl, inst)
	return client.UpdateInstanceConfig(inst.IncusName, map[string]string{
		"limits.cpu":    effective["cpu"],
		"limits.memory": effective["memory"],
	})
}
