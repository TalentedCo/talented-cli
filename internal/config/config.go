package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	DefaultAPIURL  = "https://app.talented.co"
	DefaultProfile = "default"
)

type Profile struct {
	Name    string `json:"name"`
	APIURL  string `json:"api_url"`
	Token   string `json:"token,omitempty"`
	Storage string `json:"storage,omitempty"`
}

type File struct {
	DefaultProfile string             `json:"default_profile"`
	Profiles       map[string]Profile `json:"profiles"`
}

func ConfigPath() (string, error) {
	if override := os.Getenv("TALENTED_CONFIG"); override != "" {
		return override, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "talented", "config.json"), nil
}

func Load() (File, error) {
	path, err := ConfigPath()
	if err != nil {
		return File{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{DefaultProfile: DefaultProfile, Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return File{}, err
	}
	var cfg File
	if err := json.Unmarshal(data, &cfg); err != nil {
		return File{}, err
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = DefaultProfile
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return cfg, nil
}

func Save(cfg File) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = DefaultProfile
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func ResolveProfileName(explicit string, cfg File) string {
	if explicit != "" {
		return explicit
	}
	if cfg.DefaultProfile != "" {
		return cfg.DefaultProfile
	}
	return DefaultProfile
}

func UpsertProfile(profile Profile, makeDefault bool) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if profile.Name == "" {
		profile.Name = DefaultProfile
	}
	if profile.APIURL == "" {
		profile.APIURL = DefaultAPIURL
	}
	cfg.Profiles[profile.Name] = profile
	if makeDefault || cfg.DefaultProfile == "" {
		cfg.DefaultProfile = profile.Name
	}
	return Save(cfg)
}
