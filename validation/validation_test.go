package validation

import (
	"testing"
	"net"
)

type TestPair struct {
	teststr string
	valid bool
}

func lookupIP(host string) net.IP {
	ips, err := net.LookupIP(host)
	if err != nil {
		panic(err)
	}
	if len(ips) == 0 {
		panic("No IPs for " + host)
	}
	return ips[0]
}

func TestHostnameValidation(t *testing.T) {
	testIp := lookupIP("example.com")

	tests := []TestPair{
		{"", true}, // Empty hostname is OK, we'll use clientIp
		{"127.0.0.1", false},
		{"192.168.1.2", false},
		{"localhost", false},
		{"http://example.com/", false},
		{"example.com", true},
	}

	for _, v := range tests {
		if (ValidateHostname(v.teststr, testIp) == nil) != v.valid {
			t.Error("ValidateHostname(", v.teststr, testIp, ") returned", !v.valid)
		}
	}
}

func TestLocalHostnameValidation(t *testing.T) {
	localIp := net.ParseIP("127.0.0.1")

	tests := []TestPair{
		{"", false}, // Can't use empty hostname with localhost client IP
		{"127.0.0.1", true},  // localhost is trusted to use any hostname
		{"192.168.1.2", true},
		{"localhost", true},
		{"example.com", true},
		{"http://example.com/", false},
	}

	for _, v := range tests {
		if (ValidateHostname(v.teststr, localIp) == nil) != v.valid {
			t.Error("ValidateHostname(", v.teststr, localIp, ") returned", !v.valid)
		}
	}
}


func TestSessionValidation(t *testing.T) {
	tests := []TestPair{
		{"my-custom-id-alias", true},
		{"", false},
		{"1234", true},
		{"0123456789012345678901234567890123456789", false},
		{"69f8edf9-1f79-4c80-a939-08e4e2a8fdbd", true},
	}

	for _, v := range tests {
		if isValidSessionId(v.teststr) != v.valid {
			t.Error("isValidSessionId(", v.teststr, ") returned", !v.valid)
		}
	}
}

func TestProtocolVersionValidation(t *testing.T) {
	tests := []TestPair{
		{"invalid", false},
		{"10.0", true}, // old style IDs supported for now
		{"-10.0", false},
		{"1.10.0", false}, // missing namespace
		{"dp:4.20.1", true},
	}

	for _, v := range tests {
		if IsValidProtocol(v.teststr, []string{}) != v.valid {
			t.Error("isValidProtocol(", v.teststr, ") returned", !v.valid)
		}
	}
}

func TestProtocolWhitelistValidation(t *testing.T) {
	tests := []TestPair{
		{"special", true},
		{"10.0", false},
		{"dp:4.20.1", false},
		{"dp:4.20.2", true},
	}
	whitelist := []string{"special", "dp:4.20.2"}

	for _, v := range tests {
		if IsValidProtocol(v.teststr, whitelist) != v.valid {
			t.Error("whitelist: isValidProtocol(", v.teststr, ") returned", !v.valid)
		}
	}
}

func TestIsJsFunctionName(t *testing.T) {
	tests := []TestPair{
		{"", false},
		{"fn", true},
		{"0fn", false},
		{"123", false},
		{"alert(", false},
		{"fn;fn2", false},
		{"fn2", true},
	}
	for _, v := range tests {
		if IsJsFunctionName(v.teststr) != v.valid {
			t.Error("IsJsFunctionName(", v.teststr, ") returned", !v.valid)
		}
	}
}

func TestLocalIps(t *testing.T) {
	ips := localIPs()

	hadLocalhost := false
	for _, ip := range ips {
		if ip.String() == "127.0.0.1" {
			hadLocalhost = true
			break
		}
	}
	if !hadLocalhost {
		t.Error("Localhost not found in local IP list")
	}
}

