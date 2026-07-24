// Package download fetches and unpacks MSVC/WinSDK using the same installer
// manifests Visual Studio's own installer uses.
package download

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Payload is one downloadable file belonging to a Package.
type Payload struct {
	FileName string `json:"fileName"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// Name returns the payload's bare file name, stripping any directory
// components the manifest's fileName might carry.
func (p Payload) Name() string {
	name := p.FileName
	if i := strings.LastIndexByte(name, '\\'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// Dependency is a normalized package dependency: manifests encode these
// either as a bare version string or as an object with version/type/id.
type Dependency struct {
	TargetID string // the id to depend on (overrides the map key if set)
	Version  string
	Type     string // "", "Optional" or "Recommended"
}

// LocalizedResource carries a package's human-readable title/description
// (shown by --list-workloads/--list-components) and the license URL shown
// before accepting a package's terms.
type LocalizedResource struct {
	Language    string `json:"language"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	License     string `json:"license"`
}

// Package is one entry from the installer manifest's "packages" array.
type Package struct {
	ID                 string              `json:"id"`
	Type               string              `json:"type"`
	Version            string              `json:"version"`
	Chip               string              `json:"chip"`
	MachineArch        string              `json:"machineArch"`
	ProductArch        string              `json:"productArch"`
	Language           string              `json:"language"`
	Payloads           []Payload           `json:"payloads"`
	InstallSizes       map[string]int64    `json:"installSizes"`
	LocalizedResources []LocalizedResource `json:"localizedResources"`

	DependenciesRaw map[string]json.RawMessage `json:"dependencies"`
	dependencies    map[string]Dependency
}

// Dependencies lazily normalizes DependenciesRaw into Dependency values.
func (p *Package) Dependencies() map[string]Dependency {
	if p.dependencies != nil {
		return p.dependencies
	}
	p.dependencies = map[string]Dependency{}
	for key, raw := range p.DependenciesRaw {
		var version string
		if err := json.Unmarshal(raw, &version); err == nil {
			p.dependencies[key] = Dependency{Version: version}
			continue
		}
		var d struct {
			Version string `json:"version"`
			Type    string `json:"type"`
			ID      string `json:"id"`
		}
		if err := json.Unmarshal(raw, &d); err == nil {
			p.dependencies[key] = Dependency{TargetID: d.ID, Version: d.Version, Type: d.Type}
		}
	}
	return p.dependencies
}

// Key uniquely identifies a specific package variant (id + version + arch),
// used to dedupe an already-included package during dependency resolution
// and to name its cache directory. Mirrors getPackageKey.
func (p *Package) Key() string {
	key := p.ID
	if p.Version != "" {
		key += "-" + p.Version
	}
	if p.Chip != "" {
		key += "-chip." + p.Chip
	}
	if p.MachineArch != "" {
		key += "-machineArch." + p.MachineArch
	}
	if p.ProductArch != "" {
		key += "-productArch." + p.ProductArch
	}
	return key
}

// Localized returns p's LocalizedResource best matching language ("" means
// "en"), preferring an exact match, then any en-* entry, then whatever's
// first. Returns nil if p has no localized resources at all.
func (p *Package) Localized(language string) *LocalizedResource {
	if len(p.LocalizedResources) == 0 {
		return nil
	}
	if language == "" {
		language = "en"
	}
	language = strings.ToLower(language)
	best := &p.LocalizedResources[0]
	bestScore := -1
	for i := range p.LocalizedResources {
		lr := &p.LocalizedResources[i]
		lang := strings.ToLower(lr.Language)
		score := 0
		switch {
		case lang == language:
			score = 3
		case strings.HasPrefix(lang, language+"-"):
			score = 2
		case strings.HasPrefix(lang, "en"):
			score = 1
		}
		if score > bestScore {
			bestScore = score
			best = lr
		}
	}
	return best
}

func (p *Package) InstalledSize() int64 {
	var sum int64
	for _, v := range p.InstallSizes {
		sum += v
	}
	return sum
}

func (p *Package) DownloadSize() int64 {
	var sum int64
	for _, pl := range p.Payloads {
		sum += pl.Size
	}
	return sum
}

// ChannelItem is one entry of a channel manifest's "channelItems", used only
// to locate the installer manifest URL.
type ChannelItem struct {
	Type     string    `json:"type"`
	Payloads []Payload `json:"payloads"`
}

// Manifest is the top-level installer manifest (or channel manifest, which
// shares the "info" field used for logging).
type Manifest struct {
	Info struct {
		ProductDisplayVersion string `json:"productDisplayVersion"`
	} `json:"info"`
	Packages     []Package     `json:"packages"`
	ChannelItems []ChannelItem `json:"channelItems"`
}

// Manifests (particularly the installer manifest, a single JSON file
// listing every package) can be tens of MB, so give this a generous timeout
// and a few retries - transient network hiccups shouldn't need a full
// restart of `download`.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

func init() {
	// --manifest points at a local file, fetched through this same client
	// via a "file:" URL (see cmd/vintner's runDownload) - so it needs a
	// registered "file" handler alongside the default http/https transport.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	httpClient.Transport = t
}

const maxManifestAttempts = 5

func httpGet(url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxManifestAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoff(attempt))
		}
		data, err := tryHTTPGet(url)
		if err == nil {
			return data, nil
		}
		lastErr = err
		fmt.Printf("GET %s: %v (retrying)\n", url, err)
	}
	return nil, fmt.Errorf("GET %s: giving up after %d attempts: %w", url, maxManifestAttempts, lastErr)
}

func tryHTTPGet(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// FetchChannelManifest downloads the top-level channel manifest for the
// given major VS version ("18" and up use the "stable"/"insiders" channel
// naming, earlier ones use "release"/"pre"), and returns the URL of the
// installer manifest it references.
func FetchChannelManifest(major int, preview bool) (string, error) {
	kind := "stable"
	if major < 18 {
		kind = "release"
	}
	if preview {
		kind = "insiders"
		if major < 18 {
			kind = "pre"
		}
	}
	url := fmt.Sprintf("https://aka.ms/vs/%d/%s/channel", major, kind)
	fmt.Println("Fetching", url)
	data, err := httpGet(url)
	if err != nil {
		return "", err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parsing channel manifest: %w", err)
	}
	fmt.Printf("Got toplevel manifest for %s\n", m.Info.ProductDisplayVersion)
	for _, item := range m.ChannelItems {
		if item.Type == "Manifest" && len(item.Payloads) > 0 {
			return item.Payloads[0].URL, nil
		}
	}
	return "", fmt.Errorf("unable to find an installer manifest")
}

// FetchInstallerManifest downloads and parses the installer manifest at url.
func FetchInstallerManifest(url string) (*Manifest, error) {
	data, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing installer manifest: %w", err)
	}
	fmt.Printf("Loaded installer manifest for %s\n", m.Info.ProductDisplayVersion)
	return &m, nil
}

// HumanizeBytes renders a byte count as a friendly "1.2 GB" style string.
func HumanizeBytes(s int64) string {
	switch {
	case s > 900*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(s)/(1024*1024*1024))
	case s > 900*1024:
		return fmt.Sprintf("%.1f MB", float64(s)/(1024*1024))
	case s > 1024:
		return fmt.Sprintf("%.1f KB", float64(s)/1024)
	default:
		return fmt.Sprintf("%d bytes", s)
	}
}
