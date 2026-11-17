package social

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FollowingIDs is the small second client. It reads stable content ids from
// the versioned following feed. It does not claim another protocol.
func FollowingIDs(base, token string) ([]string, error) {
	if err := loopback(base); err != nil {
		return nil, err
	}
	var doc struct {
		InterfaceVersion int  `json:"interface_version"`
		ActivityPub      bool `json:"activitypub"`
		ATProtocol       bool `json:"at_protocol"`
		ChainEvents      bool `json:"chain_events"`
	}
	if err := getJSON(base+"/v1/discovery", token, &doc); err != nil {
		return nil, err
	}
	if doc.InterfaceVersion != InterfaceVersion || doc.ActivityPub || doc.ATProtocol || doc.ChainEvents {
		return nil, errors.New("discovery document")
	}
	var feed struct {
		InterfaceVersion int    `json:"interface_version"`
		Kind             string `json:"kind"`
		ChainEvents      bool   `json:"chain_events"`
		Items            []struct {
			ContentID string `json:"content_id"`
		} `json:"items"`
	}
	if err := getJSON(base+"/v1/feeds/following", token, &feed); err != nil {
		return nil, err
	}
	if feed.InterfaceVersion != InterfaceVersion || feed.Kind != "following" || feed.ChainEvents {
		return nil, errors.New("feed")
	}
	ids := make([]string, 0, len(feed.Items))
	for _, item := range feed.Items {
		if !idRE.MatchString(item.ContentID) {
			return nil, errors.New("content id")
		}
		ids = append(ids, item.ContentID)
	}
	return ids, nil
}

func loopback(base string) error {
	parsed, err := url.Parse(base)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("base")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("base")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return errors.New("base")
	}
	return nil
}

func getJSON(raw, token string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect")
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return errors.New(strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, dst)
}
