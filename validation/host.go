package validation

import (
	"regexp"
	"net"
)

func ValidateHostname(hostname string, clientIp net.IP) error {
	// Empty hostname means we use the client IP
	if len(hostname) == 0 {
		return nil
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return ValidationError{"host", "Hostname lookup failed"}
	}

	for _, ip := range ips {
		if ip.Equal(clientIp) {
			return nil
		}
	}

	return ValidationError{"host", "Hostname does not match client IP"}
}

func IsValidProtocol(protocol string, whitelist []string) bool {
	if len(whitelist) > 0 {
		// Check against whitelist
		for _, v := range whitelist {
			if protocol == v {
				return true
			}
		}
		return false
	} else {
		// Check against generic format regex (namespace:server.major.minor)
		m, _ := regexp.MatchString(`^\w+:\d+\.\d+\.\d+$`, protocol)
		if !m {
			// Support the pre-2.0 version for now as well (major.minor)
			m, _ = regexp.MatchString(`^\d+\.\d+$`, protocol)
		}
		return m
	}
}

