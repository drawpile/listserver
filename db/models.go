package db

import (
	"fmt"
	"strings"
)

// The SessionInfo struct represents a session announcement fetched from the database
// It is also used to insert new entries.
// When inserting, the "Started" field is ignored and the current timestamp is used
type SessionInfo struct {
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Id        string   `json:"id"`
	Protocol  string   `json:"protocol"`
	Title     string   `json:"title"`
	Users     int      `json:"users"`
	Usernames []string `json:"usernames"`
	Password  bool     `json:"password"`
	Nsfm      bool     `json:"nsfm"`
	Owner     string   `json:"owner"`
	Started   string   `json:"started"`
	Roomcode  string   `json:"roomcode,omitempty"`
	Private   bool     `json:"private,omitempty"`
}

func (info SessionInfo) HostAddress() string {
	if strings.ContainsRune(info.Host, ':') {
		return fmt.Sprintf("[%s]:%d", info.Host, info.Port)
	} else {
		return fmt.Sprintf("%s:%d", info.Host, info.Port)
	}
}

// Minimum info needed to join a session
type JoinSessionInfo struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Id   string `json:"id"`
}

// Info about a newly inserted session
type NewSessionInfo struct {
	ListingId int64  `json:"id"`
	UpdateKey string `json:"key"`
	Private   bool   `json:"private"`
	Roomcode  string `json:"roomcode,omitempty"`
}

// Session list querying options
type QueryOptions struct {
	Title    string // filter by title
	Nsfm     bool   // show NSFM sessions
	Protocol string // filter by protocol version (comma separated list accepted)
}
