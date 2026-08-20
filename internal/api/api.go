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
	Name         string              `json:"name"`
	Applications []ApplicationStatus `json:"applications"`
}
type ApplicationStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	UsedSeconds  int64  `json:"used_seconds"`
	LimitSeconds int64  `json:"limit_seconds"`
	Blocked      bool   `json:"blocked"`
}
