// Package javinizer connects to a separately managed, pinned Javinizer service.
// It contains no site scraping logic and never writes Miyabi metadata.
package javinizer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
)

// Config is startup-only. The file is kept outside the repository; API tokens
// are neither exposed through Miyabi's settings API nor passed to browsers.
type Config struct {
	Enabled        bool   `json:"enabled"`
	ServerURL      string `json:"server_url"`
	APIToken       string `json:"api_token"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// LoadConfig returns a disabled extension for an unset path. An explicit but
// invalid configuration fails startup instead of silently changing providers.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("cannot read Javinizer connection config")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(body) > 8192 {
		return Config{}, errors.New("cannot read Javinizer connection config")
	}
	var config *Config
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || config == nil || decoder.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("invalid Javinizer connection config")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return *config, nil
}

func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("Javinizer server_url must be an HTTP(S) origin without credentials")
	}
	if len(c.APIToken) < 4 || len(c.APIToken) > 512 || strings.TrimSpace(c.APIToken) != c.APIToken || strings.ContainsAny(c.APIToken, "\r\n\t ") {
		return errors.New("invalid Javinizer API token")
	}
	if c.TimeoutSeconds != 0 && (c.TimeoutSeconds < 5 || c.TimeoutSeconds > 120) {
		return errors.New("Javinizer timeout must be between 5 and 120 seconds")
	}
	return nil
}
