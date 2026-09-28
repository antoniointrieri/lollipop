package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// version is set by CI on tagged builds (-ldflags "-X main.version=v0.4.0"); empty elsewhere.
var version string

const (
	latestReleaseAPI = "https://api.github.com/repos/antoniointrieri/lollipop/releases/latest"
	updateInterval   = 24 * time.Hour
)

// parseVersion reads "vMAJOR.MINOR.PATCH"; ok is false for anything else (dev builds, pseudo-versions).
func parseVersion(v string) (n [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || p != strconv.Itoa(x) {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// newer reports whether release tag is a later version than current; false when either can't be compared.
func newer(tag, current string) bool {
	a, okA := parseVersion(tag)
	b, okB := parseVersion(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// latestRelease asks GitHub for the latest published release (drafts and prereleases excluded).
func latestRelease(ctx context.Context) (tag, url string, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub: %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.TagName, r.HTMLURL, nil
}

// checkUpdates looks for a newer release at startup and then once a day, while the setting is on. Only release
// builds check: a dev build has no version to compare.
func (u *ui) checkUpdates() {
	if _, ok := parseVersion(appVersion()); !ok {
		return
	}
	time.Sleep(10 * time.Second) // not while starting up
	for {
		if u.get().CheckUpdates {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			tag, url, err := latestRelease(ctx)
			cancel()
			if err == nil && newer(tag, appVersion()) {
				u.setUpdate(tag, url)
			}
		}
		time.Sleep(updateInterval)
	}
}
