package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrGitHubStatus: GitHub answered something other than 200.
var ErrGitHubStatus = errors.New("selfupdate: GitHub answered")

const (
	defaultAPI    = "https://api.github.com"
	latestPath    = "/repos/%s/releases/latest"
	headerAccept  = "Accept"
	headerAuth    = "Authorization"
	bearerPrefix  = "Bearer "
	acceptJSON    = "application/vnd.github+json"
	acceptBinary  = "application/octet-stream"
)

// Release is one published version and its files.
type Release struct {
	Tag    string
	Assets []Asset
}

// Asset is one file of a Release; URL is the API address that serves its bytes.
type Asset struct {
	Name string
	URL  string
}

// Releases reads the published versions of the app.
type Releases interface {
	Latest() (Release, error)
	Download(a Asset) ([]byte, error)
}

// GitHub reads Releases through the GitHub REST API. Token (read-only) is
// required for a private repository.
// Example: gh := selfupdate.GitHub{Repo: "acme/shop", Token: token}
type GitHub struct {
	Repo   string       // "owner/name"
	Token  string
	API    string       // "" = https://api.github.com; tests point it at an httptest.Server
	Client *http.Client // nil = http.DefaultClient
}

var _ Releases = GitHub{}

func (g GitHub) Latest() (Release, error) {
	body, err := g.get(g.api()+fmt.Sprintf(latestPath, g.Repo), acceptJSON)
	if err != nil {
		return Release{}, err
	}
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, err
	}
	r := Release{Tag: raw.TagName}
	for _, a := range raw.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return r, nil
}

// Download returns an asset's bytes. GitHub redirects to its file storage; the
// HTTP client drops the Authorization header on that cross-host redirect.
func (g GitHub) Download(a Asset) ([]byte, error) { return g.get(a.URL, acceptBinary) }

func (g GitHub) api() string {
	if g.API != "" {
		return g.API
	}
	return defaultAPI
}

func (g GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

func (g GitHub) get(url, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerAccept, accept)
	if g.Token != "" {
		req.Header.Set(headerAuth, bearerPrefix+g.Token)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w %d for %s", ErrGitHubStatus, resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
