package download

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var reSDKVersion = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// TriState represents a `--with-*` style flag: nil means "unset, use the
// applicable default"; a set value means the user (or a higher-level
// default) explicitly chose to include/exclude the component.
type TriState = *bool

func on() TriState { v := true; return &v }

// Options holds every flag that feeds package selection and download.
type Options struct {
	Package      []string
	Ignore       []string
	Architecture []string // subset of x86,x64,arm,arm64,host
	HostArch     string   // x86, x64 or arm64
	OnlyHost     bool

	MSVCVersion string // "", "preview", "16.0".."18.0", "15.4".."15.9"
	SDKVersion  string

	WithDefault  TriState
	WithWorkload TriState
	WithMSVC     TriState
	WithASAN     TriState
	WithSDK      TriState
	WithATL      TriState
	WithDIA      TriState
	WithMSBuild  TriState
	WithDevCmd   TriState

	IncludeOptional bool
	SkipRecommended bool
	Language        string

	// WithWDK selects the PlatformToolset registration
	// (Component.Microsoft.Windows.DriverKit.BuildTools) that lets MSBuild
	// recognize WindowsKernelModeDriver10.0/WindowsUserModeDriver10.0
	// PlatformToolsets. It's not part of the manifest-driven default
	// selection - unlike everything else it depends on, it's opt-in via
	// --with-wdk, since the actual driver headers/libs come from a separate
	// NuGet package download handled outside ExpandSelection entirely (see
	// wdk.go and cmd/vintner's runDownload).
	WithWDK bool
}

func addIfWanted(opts *Options, flag TriState, pkg string) {
	if flag == nil {
		return
	}
	if *flag {
		opts.Package = append(opts.Package, pkg)
	} else {
		opts.Ignore = append(opts.Ignore, pkg)
	}
}

type msvcVersionEntry struct {
	gen         string // "15" or "16" - which package-name scheme applies
	sdk         string
	toolVersion string
}

// msvcVersionTable maps a --msvc-version value to the SDK it pulls in by
// default and the toolset version fragment used in package ids.
var msvcVersionTable = map[string]msvcVersionEntry{
	"preview": {"16", "", "Preview"},

	"16.0":  {"16", "10.0.17763", "14.20"},
	"16.1":  {"16", "10.0.18362", "14.21"},
	"16.2":  {"16", "10.0.18362", "14.22"},
	"16.3":  {"16", "10.0.18362", "14.23"},
	"16.4":  {"16", "10.0.18362", "14.24"},
	"16.5":  {"16", "10.0.18362", "14.25"},
	"16.6":  {"16", "10.0.18362", "14.26"},
	"16.7":  {"16", "10.0.18362", "14.27"},
	"16.8":  {"16", "10.0.18362", "14.28"},
	"16.9":  {"16", "10.0.19041", "14.28.16.9"},
	"16.10": {"16", "10.0.19041", "14.29.16.10"},
	"16.11": {"16", "10.0.19041", "14.29.16.11"},
	"17.0":  {"16", "10.0.19041", "14.30.17.0"},
	"17.1":  {"16", "10.0.19041", "14.31.17.1"},
	"17.2":  {"16", "10.0.19041", "14.32.17.2"},
	"17.3":  {"16", "10.0.19041", "14.33.17.3"},
	"17.4":  {"16", "10.0.22621", "14.34.17.4"},
	"17.5":  {"16", "10.0.22621", "14.35.17.5"},
	"17.6":  {"16", "10.0.22621", "14.36.17.6"},
	"17.7":  {"16", "10.0.22621", "14.37.17.7"},
	"17.8":  {"16", "10.0.22621", "14.38.17.8"},
	"17.9":  {"16", "10.0.22621", "14.39.17.9"},
	"17.10": {"16", "10.0.22621", "14.40.17.10"},
	"17.11": {"16", "10.0.22621", "14.41.17.11"},
	"17.12": {"16", "10.0.22621", "14.42.17.12"},
	"17.13": {"16", "10.0.22621", "14.43.17.13"},
	"17.14": {"16", "10.0.26100", "14.44.17.14"},
	"18.0":  {"16", "10.0.26100", "14.50.18.0"},

	"15.4": {"15", "10.0.16299", "14.11"},
	"15.5": {"15", "10.0.16299", "14.12"},
	"15.6": {"15", "10.0.16299", "14.13"},
	"15.7": {"15", "10.0.17134", "14.14"},
	"15.8": {"15", "10.0.17134", "14.15"},
	"15.9": {"15", "10.0.17763", "14.16"},
}

func selectToolsetV16(opts *Options, idx Index, userVersion, sdk, toolVersion string, defaultPkgs, defaultIgnores []string) {
	ext := ""
	if toolVersion == "Preview" {
		ext = ".Tools"
	}
	base := "Microsoft.VisualStudio.Component.VC." + toolVersion + ext
	if idx.Find(base+".x86.x64", nil) != nil {
		if contains(opts.Architecture, "x86") || contains(opts.Architecture, "x64") {
			addIfWanted(opts, opts.WithMSVC, base+".x86.x64")
			addIfWanted(opts, opts.WithASAN, "Microsoft.VC."+toolVersion+".ASAN.X86")
			addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC."+toolVersion+".ATL")
		}
		if contains(opts.Architecture, "arm") {
			addIfWanted(opts, opts.WithMSVC, "Microsoft.VisualStudio.Component.VC."+toolVersion+".ARM")
			addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC."+toolVersion+".ATL.ARM")
		}
		if contains(opts.Architecture, "arm64") {
			addIfWanted(opts, opts.WithMSVC, "Microsoft.VisualStudio.Component.VC."+toolVersion+".ARM64")
			addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC."+toolVersion+".ATL.ARM64")
		}
		if opts.SDKVersion == "" {
			opts.SDKVersion = sdk
		}
	} else {
		fmt.Printf("Didn't find exact version packages for %s, assuming this is provided by the default/latest version\n", userVersion)
		opts.Package = append(opts.Package, defaultPkgs...)
		opts.Ignore = append(opts.Ignore, defaultIgnores...)
	}
}

func selectToolsetV15(opts *Options, idx Index, userVersion, sdk, toolVersion string, defaultPkgs, defaultIgnores []string) {
	id := "Microsoft.VisualStudio.Component.VC.Tools." + toolVersion
	if idx.Find(id, nil) != nil {
		addIfWanted(opts, opts.WithMSVC, id)
		if opts.SDKVersion == "" {
			opts.SDKVersion = sdk
		}
	} else {
		fmt.Printf("Didn't find exact version packages for %s, assuming this is provided by the default/latest version\n", userVersion)
		opts.Package = append(opts.Package, defaultPkgs...)
		opts.Ignore = append(opts.Ignore, defaultIgnores...)
	}
}

// ResolveSelection turns Options' high-level flags (--with-*,
// --msvc-version, --sdk-version, explicit packages) into the final
// opts.Package/opts.Ignore lists ready for ExpandSelection.
func ResolveSelection(opts *Options, idx Index) error {
	if len(opts.Architecture) == 0 {
		opts.Architecture = []string{"host", "x86", "x64", "arm", "arm64"}
	}
	if opts.HostArch != "" && contains(opts.Architecture, "host") {
		opts.Architecture = append(opts.Architecture, opts.HostArch)
	}

	if opts.MSVCVersion != "" {
		if opts.WithMSVC == nil {
			opts.WithMSVC = on()
		}
		if opts.WithASAN == nil {
			opts.WithASAN = on()
		}
		if opts.WithATL == nil {
			opts.WithATL = on()
		}
		if opts.WithSDK == nil {
			opts.WithSDK = on()
		}
	}
	if opts.SDKVersion != "" && opts.WithSDK == nil {
		opts.WithSDK = on()
	}

	if opts.WithDefault == nil && opts.MSVCVersion == "" && len(opts.Package) == 0 {
		opts.WithDefault = on()
	}
	if opts.WithDefault != nil {
		for _, pair := range []struct {
			flag *TriState
		}{
			{&opts.WithWorkload}, {&opts.WithMSVC}, {&opts.WithASAN}, {&opts.WithSDK},
			{&opts.WithATL}, {&opts.WithDIA}, {&opts.WithMSBuild}, {&opts.WithDevCmd},
		} {
			if *pair.flag == nil {
				v := *opts.WithDefault
				*pair.flag = &v
			}
		}
	}

	defaultPkgs, defaultIgnores := opts.Package, opts.Ignore
	opts.Package, opts.Ignore = nil, nil

	addIfWanted(opts, opts.WithWorkload, "Microsoft.VisualStudio.Workload.VCTools")
	if contains(opts.Architecture, "x86") || contains(opts.Architecture, "x64") {
		addIfWanted(opts, opts.WithMSVC, "Microsoft.VisualStudio.Component.VC.Tools.x86.x64")
		addIfWanted(opts, opts.WithASAN, "Microsoft.VisualCpp.ASAN.X86")
		addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC.ATL")
	}
	if contains(opts.Architecture, "arm") {
		addIfWanted(opts, opts.WithMSVC, "Microsoft.VisualStudio.Component.VC.Tools.ARM")
		addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC.ATL.ARM")
	}
	if contains(opts.Architecture, "arm64") {
		addIfWanted(opts, opts.WithMSVC, "Microsoft.VisualStudio.Component.VC.Tools.ARM64")
		addIfWanted(opts, opts.WithATL, "Microsoft.VisualStudio.Component.VC.ATL.ARM64")
	}

	defaultPkgs, opts.Package = opts.Package, defaultPkgs
	defaultIgnores, opts.Ignore = opts.Ignore, defaultIgnores

	switch {
	case opts.MSVCVersion == "":
		opts.Package = append(opts.Package, defaultPkgs...)
		opts.Ignore = append(opts.Ignore, defaultIgnores...)
	default:
		entry, ok := msvcVersionTable[opts.MSVCVersion]
		if !ok {
			return fmt.Errorf("unsupported MSVC toolchain version %s", opts.MSVCVersion)
		}
		if entry.gen == "15" {
			selectToolsetV15(opts, idx, opts.MSVCVersion, entry.sdk, entry.toolVersion, defaultPkgs, defaultIgnores)
		} else {
			selectToolsetV16(opts, idx, opts.MSVCVersion, entry.sdk, entry.toolVersion, defaultPkgs, defaultIgnores)
		}
	}

	if err := selectSDK(opts, idx); err != nil {
		return err
	}

	addIfWanted(opts, opts.WithDIA, "Microsoft.VisualCpp.DIA.SDK")
	addIfWanted(opts, opts.WithMSBuild, "Microsoft.Build")
	addIfWanted(opts, opts.WithMSBuild, "Microsoft.Build.Dependencies")
	addIfWanted(opts, opts.WithDevCmd, "Microsoft.VisualStudio.VC.vcvars")
	addIfWanted(opts, opts.WithDevCmd, "Microsoft.VisualStudio.PackageGroup.VsDevCmd")

	if opts.WithWDK {
		opts.Package = append(opts.Package, "Component.Microsoft.Windows.DriverKit.BuildTools")
	}

	normalizeIgnoreCase(opts)
	return nil
}

func selectSDK(opts *Options, idx Index) error {
	switch {
	case opts.WithSDK == nil:
		return nil
	case !*opts.WithSDK:
		for key := range idx {
			if strings.HasPrefix(key, "win10sdk") || strings.HasPrefix(key, "win11sdk") {
				opts.Ignore = append(opts.Ignore, key)
			}
		}
	case opts.SDKVersion == "":
		saved := *opts
		opts.Package = []string{"Microsoft.VisualStudio.Workload.VCTools"}
		opts.IncludeOptional = false
		opts.SkipRecommended = false
		recommended, err := ExpandSelection(idx, opts)
		*opts = saved
		if err != nil {
			return err
		}
		for _, p := range recommended {
			key := strings.ToLower(p.ID)
			if strings.HasPrefix(key, "win10sdk") || strings.HasPrefix(key, "win11sdk") {
				opts.Package = append(opts.Package, key)
			}
		}
	default:
		found := false
		var versions []string
		for key := range idx {
			if !strings.HasPrefix(key, "win10sdk") && !strings.HasPrefix(key, "win11sdk") {
				continue
			}
			version := key[9:]
			if reSDKVersion.MatchString(version) {
				versions = append(versions, version)
			}
			if key == key[:8]+"_"+opts.SDKVersion {
				found = true
				opts.Package = append(opts.Package, key)
			} else {
				opts.Ignore = append(opts.Ignore, key)
			}
		}
		if !found {
			sort.Strings(versions)
			return fmt.Errorf("WinSDK version %s not found (available: %s)", opts.SDKVersion, strings.Join(versions, ", "))
		}
	}
	return nil
}

func normalizeIgnoreCase(opts *Options) {
	for i, s := range opts.Ignore {
		opts.Ignore[i] = strings.ToLower(s)
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// collectDependencyClosure recursively resolves target's dependency tree,
// honoring arch matching, --ignore, and Optional/Recommended filtering.
func collectDependencyClosure(idx Index, included map[string]bool, target string, constraints map[string]string, opts *Options) []*Package {
	if contains(opts.Ignore, strings.ToLower(target)) {
		return nil
	}
	p := idx.Find(target, constraints)
	if p == nil {
		return nil
	}
	if opts.OnlyHost && !HostArchCompatible(p, opts.HostArch) {
		return nil
	}
	if !TargetArchCompatible(p, opts.Architecture) {
		return nil
	}
	key := p.Key()
	if included[key] {
		return nil
	}
	included[key] = true

	ret := []*Package{p}
	deps := p.Dependencies()
	targets := make([]string, 0, len(deps))
	for target := range deps {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		dep := deps[target]
		id := target
		if dep.TargetID != "" {
			id = dep.TargetID
		}
		if dep.Type == "Optional" && !opts.IncludeOptional {
			continue
		}
		if dep.Type == "Recommended" && opts.SkipRecommended {
			continue
		}
		c := map[string]string{}
		if dep.Version != "" {
			c["version"] = dep.Version
		}
		ret = append(ret, collectDependencyClosure(idx, included, id, c, opts)...)
	}
	return ret
}

// ExpandSelection resolves opts.Package (and everything they transitively
// depend on) into a flat, deduped package list.
func ExpandSelection(idx Index, opts *Options) ([]*Package, error) {
	included := map[string]bool{}
	var ret []*Package
	for _, id := range opts.Package {
		ret = append(ret, collectDependencyClosure(idx, included, id, nil, opts)...)
	}
	return ret, nil
}

// PrintDependencyTree writes an indented tree of opts.Package and everything
// they transitively depend on to w, applying the exact same
// arch/--ignore/Optional/Recommended filtering collectDependencyClosure
// (used by ExpandSelection) does, so what's printed matches what an actual
// download would select. A package already printed once elsewhere in the
// tree is shown again as a leaf ("(see above)") rather than re-expanded, to
// keep the output finite for packages multiple components depend on.
func PrintDependencyTree(w io.Writer, idx Index, opts *Options) {
	printed := map[string]bool{}
	for _, id := range opts.Package {
		printDepNode(w, idx, id, nil, "", opts, 0, printed)
	}
}

func printDepNode(w io.Writer, idx Index, target string, constraints map[string]string, depType string, opts *Options, depth int, printed map[string]bool) {
	if contains(opts.Ignore, strings.ToLower(target)) {
		return
	}
	indent := strings.Repeat("  ", depth)
	annotation := ""
	if depType != "" {
		annotation = " [" + depType + "]"
	}

	p := idx.Find(target, constraints)
	if p == nil {
		fmt.Fprintf(w, "%s%s (not found)%s\n", indent, target, annotation)
		return
	}
	if opts.OnlyHost && !HostArchCompatible(p, opts.HostArch) {
		return
	}
	if !TargetArchCompatible(p, opts.Architecture) {
		return
	}

	key := p.Key()
	if printed[key] {
		fmt.Fprintf(w, "%s%s%s (see above)\n", indent, p.ID, annotation)
		return
	}
	printed[key] = true
	fmt.Fprintf(w, "%s%s@%s%s\n", indent, p.ID, p.Version, annotation)

	deps := p.Dependencies()
	targets := make([]string, 0, len(deps))
	for t := range deps {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, depTarget := range targets {
		dep := deps[depTarget]
		id := depTarget
		if dep.TargetID != "" {
			id = dep.TargetID
		}
		if dep.Type == "Optional" && !opts.IncludeOptional {
			continue
		}
		if dep.Type == "Recommended" && opts.SkipRecommended {
			continue
		}
		c := map[string]string{}
		if dep.Version != "" {
			c["version"] = dep.Version
		}
		printDepNode(w, idx, id, c, dep.Type, opts, depth+1, printed)
	}
}
