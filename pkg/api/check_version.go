package api

import (
	"encoding/json"
	"fmt"
	"github.com/stashapp/stash/pkg/logger"
	"io/ioutil"
	"net/http"
	"runtime"
	"time"
)

//we use the github REST V3 API as no login is required
const apiURL string = "https://api.github.com/repos/stashapp/stash/tags"
const apiReleases string = "https://api.github.com/repos/stashapp/stash/releases"
const apiAcceptHeader string = "application/vnd.github.v3+json"

var stashReleases = func() map[string]string {
	return map[string]string{
		"windows/amd64": "stash-win.exe",
		"linux/amd64":   "stash-linux",
		"darwin/amd64":  "stash-osx",
		"linux/arm":     "stash-pi",
	}
}

type githubTagResponse struct {
	Name        string
	Zipball_url string
	Tarball_url string
	Commit      struct {
		Sha string
		Url string
	}
	Node_id string
}

type githubReleasesResponse struct {
	Url              string
	Assets_url       string
	Upload_url       string
	Html_url         string
	Id               int64
	Node_id          string
	Tag_name         string
	Target_commitish string
	Name             string
	Draft            bool
	Author           githubAuthor
	Prerelease       bool
	Created_at       string
	Published_at     string
	Assets           []githubAsset
	Tarball_url      string
	Zipball_url      string
	Body             string
}

type githubAuthor struct {
	Login               string
	Id                  int64
	Node_id             string
	Avatar_url          string
	Gravatar_id         string
	Url                 string
	Html_url            string
	Followers_url       string
	Following_url       string
	Gists_url           string
	Starred_url         string
	Subscriptions_url   string
	Organizations_url   string
	Repos_url           string
	Events_url          string
	Received_events_url string
	Type                string
	Site_admin          bool
}

type githubAsset struct {
	Url                  string
	Id                   int64
	Node_id              string
	Name                 string
	Label                string
	Uploader             githubAuthor
	Content_type         string
	State                string
	Size                 int64
	Download_count       int64
	Created_at           string
	Updated_at           string
	Browser_download_url string
}

//gets latest version (git commit hash) from github API
//the repo's tags are used to find the latest version
//of the "master" or "develop" branch
func GetLatestVersion(shortHash bool) (latestVersion string, latestRelease string, err error) {

	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	wantedRelease := stashReleases()[platform]

	branch, _, _ := GetVersion()
	if branch == "" {
		return "", "", fmt.Errorf("Stash doesn't have a version. Version check not supported.")
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	req, _ := http.NewRequest("GET", apiReleases, nil)

	req.Header.Add("Accept", apiAcceptHeader) // gh api recommendation , send header with api version
	response, err := client.Do(req)

	input := make([]githubReleasesResponse, 0)

	if err != nil {
		return "", "", fmt.Errorf("Github API request failed: %s", err)
	} else {

		defer response.Body.Close()

		data, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return "", "", fmt.Errorf("Github API read response failed: %s", err)
		} else {
			err = json.Unmarshal(data, &input)
			if err != nil {
				return "", "", fmt.Errorf("Unmarshalling Github API response failed: %s", err)
			} else {

				for _, ghApi := range input {
					if ghApi.Tag_name == branch {

						if shortHash {
							latestVersion = ghApi.Target_commitish[0:7] //shorthash is first 7 digits of git commit hash
						} else {
							latestVersion = ghApi.Target_commitish
						}
						if wantedRelease != "" {
							for _, asset := range ghApi.Assets {
								if asset.Name == wantedRelease {
									latestRelease = asset.Browser_download_url
									break
								}

							}
						}
						break
					}
				}

			}
		}
		if latestVersion == "" {
			return "", "", fmt.Errorf("No version found for \"%s\"", branch)
		}
	}
	return latestVersion, latestRelease, nil

}

func printLatestVersion() {
	_, githash, _ = GetVersion()
	latest, _, err := GetLatestVersion(true)
	if err != nil {
		logger.Errorf("Couldn't find latest version: %s", err)
	} else {
		if githash == latest {
			logger.Infof("Version: (%s) is already the latest released.", latest)
		} else {
			logger.Infof("New version: (%s) available.", latest)
		}
	}

}
