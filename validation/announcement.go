package validation

import (
	"github.com/drawpile/listserver/db"
	"net"
)

type AnnouncementValidationRules struct {
	ClientIP            net.IP
	AllowWellKnownPorts bool
	ProtocolWhitelist   []string
}

func ValidateAnnouncement(session db.SessionInfo, rules AnnouncementValidationRules) error {
	// Hostname (if present) must be valid
	if err := ValidateHostname(session.Host, rules.ClientIP); err != nil {
		return err
	}

	// Port number must be in the valid range
	if session.Port < 0 || session.Port > 0xffff {
		return ValidationError{"port", "invalid number"}
	}
	if !rules.AllowWellKnownPorts && session.Port != 0 && session.Port < 1024 {
		return ValidationError{"port", "range 1-1024 not allowed"}
	}

	// Session ID may consist of characters a-z, A-Z and '-'
	if len(session.Id) < 1 || len(session.Id) > 36 {
		return ValidationError{"id", "invalid ID"}
	}

	// Protocol version number must be syntactically correct
	if !IsValidProtocol(session.Protocol, rules.ProtocolWhitelist) {
		return ValidationError{"protocol", "unsupported protocol version"}
	}

	return nil
}

func isValidSessionId(id string) bool {
	if len(id) < 1 || len(id) > 36 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
