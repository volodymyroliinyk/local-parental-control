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
	Date  string       `json:"date"`
	Users []UserStatus `json:"users"`
}
type UserStatus struct {
	Name               string              `json:"name"`
	DeviceUsedSeconds  int64               `json:"device_used_seconds"`
	DeviceLimitSeconds int64               `json:"device_limit_seconds"`
	AllowedFrom        string              `json:"allowed_from"`
	AllowedUntil       string              `json:"allowed_until"`
	DeviceBlocked      bool                `json:"device_blocked"`
	Applications       []ApplicationStatus `json:"applications"`
}
type ApplicationStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	UsedSeconds  int64  `json:"used_seconds"`
	LimitSeconds int64  `json:"limit_seconds"`
	Blocked      bool   `json:"blocked"`
}
