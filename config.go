package main

import "github.com/BurntSushi/toml"

type config struct {
	Listen string
	Database string
	Name string
	Description string
	Favicon string
	Welcome string
	AllowWellKnownPorts bool
	ProtocolWhitelist []string
	MaxSessionsPerHost int
	TrustedHosts []string
	RemoteAddressHeader string
	CheckUserAgent bool
}

func (c *config) IsTrustedHost(host string) bool {
	for _, v := range c.TrustedHosts {
		if host == v {
			return true
		}
	}
	return false
}

func defaultConfig() *config {
	return &config{
		Listen: "localhost:8080",
		Database: "",
		Name: "test",
		Description: "Test server",
		Favicon: "",
		Welcome: "",
		AllowWellKnownPorts: false,
		ProtocolWhitelist: []string{},
		MaxSessionsPerHost: 3,
		TrustedHosts: []string{},
		RemoteAddressHeader: "",
		CheckUserAgent: false,
	}
}

func readConfigFile(path string) (*config, error) {
	cfg := defaultConfig()

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

