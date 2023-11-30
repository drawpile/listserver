package inclsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/drawpile/listserver/db"
)

type statusServerResponse struct {
	Hostname string `json:"ext_host"`
	Port     int    `json:"ext_port"`
}

type sessionServerResponse struct {
	Alias                  string
	Closed                 bool
	Founder                string
	HasPassword            bool
	Id                     string
	MaxUserCount           int
	Nsfm                   bool
	Persistent             bool
	Protocol               string
	Size                   int
	StartTime              string
	Title                  string
	UserCount              int
	ActiveDrawingUserCount int
	AllowWeb               bool
}

type cachedSessionInfos struct {
	Time        time.Time
	Host        string
	Port        int
	SessionInfo []db.SessionInfo
}

type sessionFetchResult struct {
	index    int
	sessions []db.SessionInfo
}

var cache = map[string]cachedSessionInfos{}
var cacheMutex = sync.Mutex{}
var CacheTtl = time.Duration(0)
var StatusCacheTtl = time.Duration(0)
var Timeout = time.Duration(0)

func (ssr *sessionServerResponse) AliasOrId() string {
	if ssr.Alias != "" {
		return ssr.Alias
	} else {
		return ssr.Id
	}
}

func (ssr *sessionServerResponse) UnmarshalJSON(data []byte) error {
	ssr.ActiveDrawingUserCount = -1
	type jsonSessionServerResponse sessionServerResponse
	return json.Unmarshal(data, (*jsonSessionServerResponse)(ssr))
}

func fetchJson(url string, v interface{}) error {
	client := http.Client{Timeout: Timeout}
	resp, err := client.Get(url)
	if err != nil {
		log.Println(url, "fetch error:", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println(url, "got status", resp.Status)
		return errors.New("Server returned bad status")
	}

	body, err := io.ReadAll(resp.Body)

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
func fetchServerSessionList(urlString string, host string, port int) ([]db.SessionInfo, string, int, error) {
	var err error

	if host == "" {
		var info statusServerResponse
		if err = fetchJson(urlString+"/status/", &info); err != nil {
			return nil, "", 0, err
		}
		host = info.Hostname
		port = info.Port
	}

	var listResponse []sessionServerResponse
	if err = fetchJson(urlString+"/sessions/", &listResponse); err != nil {
		return nil, "", 0, err
	}

	sessions := make([]db.SessionInfo, len(listResponse))
	for i, v := range listResponse {
		sessions[i] = db.SessionInfo{
			Host:               host,
			Port:               port,
			Id:                 v.AliasOrId(),
			Protocol:           v.Protocol,
			Title:              v.Title,
			Users:              v.UserCount,
			Usernames:          []string{},
			Password:           v.HasPassword,
			Nsfm:               v.Nsfm,
			Owner:              v.Founder,
			Started:            v.StartTime,
			Roomcode:           "",
			Private:            false,
			MaxUsers:           v.MaxUserCount,
			Closed:             v.Closed,
			ActiveDrawingUsers: v.ActiveDrawingUserCount,
			AllowWeb:           v.AllowWeb,
		}
	}

	return sessions, host, port, nil
}

func getCached(urlString string) (cachedSessionInfos, bool) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	value, found := cache[urlString]
	return value, found
}

func putCached(urlString string, host string, port int, sessions []db.SessionInfo) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cache[urlString] = cachedSessionInfos{
		Time:        time.Now(),
		Host:        host,
		Port:        port,
		SessionInfo: sessions,
	}
}

func cachedFetchServerSessionList(urlString string) ([]db.SessionInfo, error) {
	if CacheTtl > 0 {
		cachedHost := ""
		cachedPort := 0
		value, found := getCached(urlString)
		if found {
			dt := time.Now().Sub(value.Time)
			if dt <= CacheTtl {
				return value.SessionInfo, nil
			}
			if dt <= StatusCacheTtl {
				cachedHost = value.Host
				cachedPort = value.Port
			}
		}

		sessions, host, port, err := fetchServerSessionList(urlString, cachedHost, cachedPort)
		if err != nil {
			return nil, err
		}

		putCached(urlString, host, port, sessions)
		return sessions, nil
	} else {
		sessions, host, port, err := fetchServerSessionList(urlString, "", 0)
		putCached(urlString, host, port, nil)
		return sessions, err
	}
}

// Fetch a list of sessions from the given Drawpile server admin API URLs.
// A BASIC Auth username:password pair can be included in the URL if needed.
func FetchServerAdminSessionList(urlString string) ([]db.AdminSession, error) {
	var err error
	var info statusServerResponse

	if err = fetchJson(urlString+"/status/", &info); err != nil {
		return nil, err
	}

	var listResponse []sessionServerResponse
	if err = fetchJson(urlString+"/sessions/", &listResponse); err != nil {
		return nil, err
	}

	sessions := make([]db.AdminSession, len(listResponse))
	for i, v := range listResponse {
		sessions[i] = db.AdminSession{
			Host:               info.Hostname,
			Port:               info.Port,
			SessionId:          v.Id,
			Protocol:           v.Protocol,
			Title:              v.Title,
			Users:              v.UserCount,
			Usernames:          []string{},
			Password:           v.HasPassword,
			Nsfm:               v.Nsfm,
			Owner:              v.Founder,
			Started:            v.StartTime,
			Alias:              v.Alias,
			Private:            false,
			MaxUsers:           v.MaxUserCount,
			Closed:             v.Closed,
			Included:           true,
			ActiveDrawingUsers: v.ActiveDrawingUserCount,
		}
	}

	return sessions, nil
}

func filterSessionList(sessions []db.SessionInfo, opts db.QueryOptions) []db.SessionInfo {
	filtered := []db.SessionInfo{}
	for _, s := range sessions {
		if (opts.Title == "" || strings.Contains(s.Title, opts.Title)) &&
			(opts.Nsfm || !s.Nsfm) &&
			(opts.Protocol == "" || strings.Contains(s.Protocol, opts.Protocol)) {

			filtered = append(filtered, s)
		}
	}
	return filtered
}

func asyncFetchSessionList(opts db.QueryOptions, url string, ch chan sessionFetchResult, index int) {
	defer func() {
		if err := recover(); err != nil {
			ch <- sessionFetchResult{index, nil}
			log.Printf("Panic including %v: %v\n", url, err)
		}
	}()
	ses, err := cachedFetchServerSessionList(url)
	if err != nil {
		log.Printf("Error including %v: %v\n", url, err)
		ch <- sessionFetchResult{index, ses}
	} else {
		ch <- sessionFetchResult{index, filterSessionList(ses, opts)}
	}
}

func FetchFilteredSessionLists(opts db.QueryOptions, urls ...string) []db.SessionInfo {
	urlCount := len(urls)
	if urlCount == 0 {
		return []db.SessionInfo{}
	} else if urlCount == 1 {
		url := urls[0]
		sessions, err := cachedFetchServerSessionList(url)
		if err != nil {
			log.Printf("Error including %v: %v\n", url, err)
			return []db.SessionInfo{}
		}
		return filterSessionList(sessions, opts)
	} else {
		ch := make(chan sessionFetchResult, urlCount)
		for index, url := range urls {
			go asyncFetchSessionList(opts, url, ch, index)
		}

		results := make([]sessionFetchResult, urlCount)
		sessionCount := 0
		for i := 0; i < urlCount; i++ {
			result := <-ch
			results[result.index] = result
			if result.sessions != nil {
				sessionCount += len(result.sessions)
			}
		}

		if sessionCount == 0 {
			return []db.SessionInfo{}
		}
		sessions := make([]db.SessionInfo, 0, sessionCount)
		for _, result := range results {
			if result.sessions != nil {
				sessions = append(sessions, result.sessions...)
			}
		}
		return sessions
	}
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

func IncludeHosts() []string {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	hosts := make([]string, len(cache))
	for _, value := range cache {
		hosts = append(hosts, strings.ToLower(value.Host))
	}
	return hosts
}
