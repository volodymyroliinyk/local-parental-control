package api

type Request struct {
	Command     string `json:"command"`
	User        string `json:"user,omitempty"`
	Application string `json:"application,omitempty"`
}

type Response struct {
	OK      bool    `json:"ok"`
	Message string  `json:"message,omitempty"`
	Error   string  `json:"error,omitempty"`
	Status  *Status `json:"status,omitempty"`
}

type Status struct {
	Date             string       `json:"date"`
	RecoveryRequired bool         `json:"recovery_required,omitempty"`
	RecoveryReason   string       `json:"recovery_reason,omitempty"`
	Users            []UserStatus `json:"users"`
}
type UserStatus struct {
	Name                   string              `json:"name"`
	DeviceUsedSeconds      int64               `json:"device_used_seconds"`
	DeviceLimitSeconds     int64               `json:"device_limit_seconds"`
	AllDay                 bool                `json:"all_day"`
	AllowedFrom            string              `json:"allowed_from"`
	AllowedUntil           string              `json:"allowed_until"`
	DeviceBlocked          bool                `json:"device_blocked"`
	ContinuousUsedSeconds  int64               `json:"continuous_used_seconds"`
	ContinuousLimitSeconds int64               `json:"continuous_limit_seconds"`
	BreakUntil             string              `json:"break_until,omitempty"`
	RecoveryRequired       bool                `json:"recovery_required,omitempty"`
	Applications           []ApplicationStatus `json:"applications"`
}
type ApplicationStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	UsedSeconds  int64  `json:"used_seconds"`
	LimitSeconds int64  `json:"limit_seconds"`
	Blocked      bool   `json:"blocked"`
}
