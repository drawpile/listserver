package main

import (
	"github.com/BurntSushi/toml"
	"strings"
)

type config struct {
	Listen                  string
	Database                string
	Name                    string
	Description             string
	Favicon                 string
	Welcome                 string
	NsfmWords               []string
	AllowWellKnownPorts     bool
	ProtocolWhitelist       []string
	MaxSessionsPerHost      int
	MaxSessionsPerNamedHost int
	TrustedHosts            []string
	BannedHosts             []string
	RemoteAddressHeader     string
	CheckUserAgent          bool
	WarnIpv6                bool
	Roomcodes               bool
	CheckServer             bool
}

func (c *config) IsTrustedHost(host string) bool {
	for _, v := range c.TrustedHosts {
		if host == v {
			return true
		}
	}
	return false
}

func (c *config) IsBannedHost(host string) bool {
	for _, v := range c.BannedHosts {
		if host == v {
			return true
		}
	}
	return false
}

func (c *config) ContainsNsfmWords(str string) bool {
	str = strings.ToUpper(str)
	for _, s := range c.NsfmWords {
		if strings.Contains(str, s) {
			return true
		}
	}
	return false
}

func defaultConfig() *config {
	return &config{
		Listen:                  "localhost:8080",
		Database:                "",
		Name:                    "test",
		Description:             "Test server",
		Favicon:                 "",
		Welcome:                 "",
		NsfmWords:               []string{"18+", "NSFW", "NSFM"},
		AllowWellKnownPorts:     false,
		ProtocolWhitelist:       []string{},
		MaxSessionsPerHost:      3,
		MaxSessionsPerNamedHost: 10,
		TrustedHosts:            []string{},
		BannedHosts:             []string{},
		RemoteAddressHeader:     "",
		CheckUserAgent:          false,
		WarnIpv6:                true,
		Roomcodes:               true,
		CheckServer:             true,
	}
}

func readConfigFile(path string) (*config, error) {
	cfg := defaultConfig()

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}

	// Normalizations
	for i, s := range cfg.NsfmWords {
		cfg.NsfmWords[i] = strings.ToUpper(s)
	}

	if cfg.MaxSessionsPerNamedHost < cfg.MaxSessionsPerHost {
		cfg.MaxSessionsPerNamedHost = cfg.MaxSessionsPerHost
	}

	return cfg, nil
}
