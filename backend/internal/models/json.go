package models

import "encoding/json"

// InstanceJSON is the JSON-friendly representation of Instance.
type InstanceJSON struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	UserID       int64   `json:"user_id"`
	TemplateID   int64   `json:"template_id"`
	ServerID     int64   `json:"server_id"`
	VolumeID     *int64  `json:"volume_id"`
	IncusName    string  `json:"incus_name"`
	Status       string  `json:"status"`
	IPAddress    *string `json:"ip_address"`
	LastActiveAt *string `json:"last_active_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`

	SleepDisabled       bool `json:"sleep_disabled"`
	SleepTimeoutMinutes int  `json:"sleep_timeout_minutes"`
}

func (inst Instance) MarshalJSON() ([]byte, error) {
	j := InstanceJSON{
		ID:         inst.ID,
		Name:       inst.Name,
		UserID:     inst.UserID,
		TemplateID: inst.TemplateID,
		ServerID:   inst.ServerID,
		IncusName:  inst.IncusName,
		Status:     inst.Status,
		CreatedAt:  inst.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:  inst.UpdatedAt.Format("2006-01-02T15:04:05Z"),

		SleepDisabled:       inst.SleepDisabled,
		SleepTimeoutMinutes: inst.SleepTimeoutMinutes,
	}
	if inst.VolumeID.Valid {
		j.VolumeID = &inst.VolumeID.Int64
	}
	if inst.IPAddress.Valid && inst.IPAddress.String != "" {
		j.IPAddress = &inst.IPAddress.String
	}
	if inst.LastActiveAt.Valid {
		t := inst.LastActiveAt.Time.Format("2006-01-02T15:04:05Z")
		j.LastActiveAt = &t
	}
	return json.Marshal(j)
}

// MarshalJSON emits the embedded Instance's own JSON form plus the admin-only
// fields. Instance defines MarshalJSON, and an embedding struct promotes that
// method — without this override the extra columns would be silently dropped.
func (inst AdminInstance) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(inst.Instance)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	fields["user_email"] = inst.UserEmail
	fields["user_name"] = inst.UserName
	fields["template_name"] = inst.TemplateName
	return json.Marshal(fields)
}
