package mdcz

import (
	"encoding/json"
	"errors"
	"os"
)

// bootstrapConnection is optional package provisioning, used only before the
// user has saved extension settings. It never changes enabled/translation flags
// or edits the database behind the settings API.
func bootstrapConnection(current Settings, path string) (Settings, error) {
	if path == "" {
		return current, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return current, errors.New("cannot read MDCz bootstrap connection")
	}
	var connection struct {
		ServerURL string `json:"server_url"`
		APIKey    string `json:"api_key"`
	}
	if len(body) > 8192 || json.Unmarshal(body, &connection) != nil {
		return current, errors.New("invalid MDCz bootstrap connection")
	}
	next := current
	next.ServerURL, next.APIKey = connection.ServerURL, connection.APIKey
	check := next
	check.Enabled = true // Require a complete connection even while disabled.
	if err := validate(check); err != nil {
		return current, errors.New("invalid MDCz bootstrap connection")
	}
	return next, nil
}
