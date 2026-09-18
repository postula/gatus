package visibility

type Visibility struct {
	Public bool `yaml:"public,omitempty" json:"public,omitempty"`

	// Badges exposes badges, raw uptime/response time and charts of a non-public endpoint to unauthenticated users
	Badges bool `yaml:"badges,omitempty" json:"badges,omitempty"`
}
