package validation

import (
	"net"
	"regexp"
	"strings"
)

func ValidateHostname(hostname string, clientIp net.IP) error {
	isLocalIp := isLocalIp(clientIp)

	// Empty hostname means we use the client IP
	if len(hostname) == 0 {
		// Except if client IP is localhost
		if isLocalIp {
			return ValidationError{"host", "hostname must be set when announcing from localhost"}
		} else {
			return nil
		}
	}

	// Validate hostname syntax
	if m, _ := regexp.MatchString(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`, hostname); !m {
		return ValidationError{"host", "Invalid hostname"}
	}

	// Check that the hostname actually resolves
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return ValidationError{"host", "Hostname lookup failed"}
	}

	// If client IP is localhost, allow any valid hostname
	// (We could have a list of allowed local hostnames, but generally
	//  if the server is running on localhost, we can trust it.)
	if isLocalIp {
		return nil
	}

	// For non-localhosts, hostname must resolve to the client IP
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

func IsNamedHost(hostname string) bool {
	// Just check that the hostname ends with something that looks like a toplevel domain
	// We assume the hostname has been validated before and is either an IP address or
	// a valid domain name.
	m, _ := regexp.MatchString(`^.+\.[A-Za-z]+$`, hostname)
	return m
}

// Check if the given hostname is found in the list of hostnames.
// Hostname list should be in all lower case.
// Wildcard prefix (*.example.com) is allowed in host list
func IsHostInList(hostname string, hostlist []string) bool {
	host := strings.ToLower(hostname)

	for _, item := range hostlist {
		if strings.HasPrefix(item, "*") {
			if strings.HasSuffix(host, item[1:]) {
				return true
			}
		} else {
			if host == item {
				return true
			}
		}
	}
	return false
}

func IsIpv6Address(address string) bool {
	// A bit sloppy: we assume the address is either a valid hostname or an IPv4/IPv6 address
	m, _ := regexp.MatchString(`^[0-9a-fA-F]+:`, address)
	return m
}

func isLocalIp(clientIp net.IP) bool {
	for _, ip := range localIPs() {
		switch v := ip.(type) {
		case *net.IPNet:
			if v.Contains(clientIp) {
				return true
			}
		case *net.IPAddr:
			if v.IP.Equal(clientIp) {
				return true
			}
		}
	}
	return false
}

func localIPs() (ips []net.Addr) {
	ips = make([]net.Addr, 0)

	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			panic(err)
		}
		ips = append(ips, addrs...)
	}
	return
}
