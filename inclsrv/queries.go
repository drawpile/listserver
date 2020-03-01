package inclsrv

import (
	"net/http"
	"encoding/json"
	"io/ioutil"
	"strings"
	"errors"
	"log"
	"fmt"

	"github.com/drawpile/listserver/db"
)

type statusServerResponse struct {
	Hostname string `json:"ext_host"`
	Port int `json:"ext_port"`
}

type sessionServerResponse struct {
	Alias string
	Closed bool
	Founder string
	HasPassword bool
	Id string
	MaxUserCount int
	Nsfm bool
	Persistent bool
	Protocol string
	Size int
	StartTime string
	Title string
	UserCount int
}

func (ssr *sessionServerResponse) AliasOrId() string {
	if ssr.Alias != "" {
		return ssr.Alias;
	} else {
		return ssr.Id;
	}
}

func fetchJson(url string, v interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		log.Println(url, "fetch error:", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println(url, "got status", resp.Status)
		return errors.New("Server returned bad status")
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Println(url, "read error:", err)
		return err
	}

	err = json.Unmarshal(body, v)
	if err != nil {
		log.Println(url, "parse error:", err)
		return err
	}

	return nil
}

// Fetch a list of sessions from the given Drawpile server admin API URLs.
// A BASIC Auth username:password pair can be included in the URL if needed.
func FetchServerSessionList(urlString string) ([]db.SessionInfo, error) {
	var err error
	var info statusServerResponse

	if err = fetchJson(urlString + "/status/", &info); err != nil {
		return nil, err
	}

	var listResponse []sessionServerResponse
	if err = fetchJson(urlString + "/sessions/", &listResponse); err != nil {
		return nil, err
	}

	sessions := make([]db.SessionInfo, len(listResponse))
	for i, v := range listResponse {
		sessions[i] = db.SessionInfo{
			Host: info.Hostname,
			Port: info.Port,
			Id: v.AliasOrId(),
			Protocol: v.Protocol,
			Title: v.Title,
			Users: v.UserCount,
			Usernames: []string{},
			Password: v.HasPassword,
			Nsfm: v.Nsfm,
			Owner: v.Founder,
			Started: v.StartTime,
			Roomcode: "",
			Private: false,
		}
	}

	return sessions, nil
}

func filterSessionList(sessions []db.SessionInfo, opts db.QueryOptions) []db.SessionInfo {
	filtered := []db.SessionInfo{}
	for _, s := range sessions {
		if
			(opts.Title == "" || strings.Contains(s.Title, opts.Title)) &&
			(opts.Nsfm || !s.Nsfm) &&
			(opts.Protocol == "" || strings.Contains(s.Protocol, opts.Protocol)) {

			filtered = append(filtered, s)
		}
	}
	return filtered
}

func FetchFilteredSessionLists(opts db.QueryOptions, urls ...string) []db.SessionInfo {
	var sessions []db.SessionInfo

	for _, url := range urls {
		ses, err := FetchServerSessionList(url)
		if err != nil {
			continue
		}
		ses = filterSessionList(ses, opts)
		if len(ses) > 0 {
			sessions = append(sessions, ses...)
		}
	}

	return sessions
}

func MergeLists(lists ...[]db.SessionInfo) []db.SessionInfo {
	ids := make(map[string]bool)
	merged := []db.SessionInfo{}

	// Deduplicate list in case the session is also announced at this server
	for _, sessionlist := range lists {
		for _, session := range sessionlist {
			key := fmt.Sprintf("%s-%d-%s", session.Host, session.Port, session.Id)
			if !ids[key] {
				ids[key] = true
				merged = append(merged, session)
			}
		}
	}

	return merged
}

