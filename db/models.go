package db

// The SessionInfo struct represents a session announcement fetched from the database
// It is also used to insert new entries.
// When inserting, the "Started" field is ignored and the current timestamp is used
type SessionInfo struct {
	Host string `json:"host"`
	Port int `json:"port"`
	Id string `json:"id"`
	Protocol string `json:"protocol"`
	Title string `json:"title"`
	Users int `json:"users"`
	Usernames []string `json:"usernames"`
	Password bool `json:"password"`
	Nsfm bool `json:"nsfm"`
	Owner string `json:"owner"`
	Started string `json:"started"`
}

// Info about a newly inserted session
type NewSessionInfo struct {
	ListingId int `json:"id"`
	UpdateKey string `json:"key"`
}

