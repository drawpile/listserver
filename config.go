package main

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kelseyhightower/envconfig"
)

type config struct {
	Listen                  string
	IncludeServers          []string
	AllowOrigins            []string
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
	ProxyHeaders            bool
	WarnIpv6                bool
	Public                  bool
	Roomcodes               bool
	CheckServer             bool
	SessionTimeout          int
	ShutdownTimeout         int
	LogRequests             bool
	EnableAdminApi          bool
	IncludeCacheTtl         int
	IncludeStatusCacheTtl   int
}

func (c *config) IsTrustedHost(host string) bool {
	for _, v := range c.TrustedHosts {
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
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "Unconfigured server"
	}

	return &config{
		Listen:                  "localhost:8080",
		IncludeServers:          []string{},
		AllowOrigins:            []string{"*"},
		Database:                "memory",
		Name:                    hostname,
		Description:             "A Drawpile listing server",
		Favicon:                 "",
		Welcome:                 "",
		NsfmWords:               []string{"18+", "NSFW", "NSFM"},
		AllowWellKnownPorts:     false,
		ProtocolWhitelist:       []string{},
		MaxSessionsPerHost:      3,
		MaxSessionsPerNamedHost: 10,
		TrustedHosts:            []string{},
		BannedHosts:             []string{},
		ProxyHeaders:            false,
		WarnIpv6:                true,
		Public:                  true,
		Roomcodes:               true,
		CheckServer:             true,
		SessionTimeout:          10,
		ShutdownTimeout:         1,
		LogRequests:             false,
		EnableAdminApi:          false,
		IncludeCacheTtl:         0,
		IncludeStatusCacheTtl:   0,
	}
}

func readConfigFile(path string) (*config, error) {
	cfg := defaultConfig()

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}

	doNormalizations(cfg)

	return cfg, nil
}

func readEnv() (*config, error) {
	cfg := defaultConfig()

	if err := envconfig.Process("ls", cfg); err != nil {
		return nil, err
	}

	doNormalizations(cfg)

	return cfg, nil
}

func doNormalizations(cfg *config) {
	for i, s := range cfg.NsfmWords {
		cfg.NsfmWords[i] = strings.ToUpper(s)
	}

	if cfg.MaxSessionsPerNamedHost < cfg.MaxSessionsPerHost {
		cfg.MaxSessionsPerNamedHost = cfg.MaxSessionsPerHost
	}

	for i, h := range cfg.BannedHosts {
		cfg.BannedHosts[i] = strings.ToLower(h)
	}
	for i, h := range cfg.TrustedHosts {
		cfg.TrustedHosts[i] = strings.ToLower(h)
	}

	if cfg.IncludeStatusCacheTtl < cfg.IncludeCacheTtl {
		cfg.IncludeStatusCacheTtl = cfg.IncludeCacheTtl
	}
}
