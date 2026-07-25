package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Cheviiot/vintner/internal/download"
	"github.com/Cheviiot/vintner/internal/i18n"
	"github.com/Cheviiot/vintner/internal/lock"
)

func runDownload(args []string) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	dest := fs.String("dest", "", "directory to install into (default: ~/.vintner)")
	cacheDir := fs.String("cache", "", "directory to use as a persistent download cache (default: a temp dir, removed afterwards)")
	major := fs.Int("major", 18, "the major VS version to download")
	preview := fs.Bool("preview", false, "download the preview/insiders channel instead of release/stable")
	manifestFile := fs.String("manifest", "", "use a predownloaded installer manifest file instead of fetching one")
	acceptLicense := fs.Bool("accept-license", false, "don't prompt for accepting the license")
	msvcVersion := fs.String("msvc-version", "", "install a specific MSVC toolchain version, e.g. 17.10")
	sdkVersion := fs.String("sdk-version", "", "install a specific Windows SDK version")
	hostArch := fs.String("host-arch", "", "host architecture of packages to install (x86, x64, arm64; auto-detected)")
	onlyHost := fs.Bool("only-host", true, "only download packages matching the host architecture")
	language := fs.String("language", "en", "preferred language code for packages available in multiple languages")
	includeOptional := fs.Bool("include-optional", false, "include all optional dependencies")
	skipRecommended := fs.Bool("skip-recommended", false, "don't include recommended dependencies")
	onlyDownload := fs.Bool("only-download", false, "stop after downloading package files")
	onlyUnpack := fs.Bool("only-unpack", false, "unpack selected packages and keep everything, without pruning to just the CLI tools")
	keepUnpack := fs.Bool("keep-unpack", false, "keep the scratch unpack dir instead of removing it after moving files into place")
	skipPatch := fs.Bool("skip-patch", false, "don't apply the Wine compatibility patches")
	listWorkloads := fs.Bool("list-workloads", false, "list available workloads from the manifest and exit, without downloading anything")
	listComponents := fs.Bool("list-components", false, "list available components from the manifest and exit, without downloading anything")
	printDepsTree := fs.Bool("print-deps-tree", false, "print the dependency tree of the selected packages and exit, without downloading anything")
	withWDK := fs.Bool("with-wdk", false, "also fetch and install the Windows Driver Kit (headers, libs and MSBuild driver PlatformToolsets, for building KMDF/UMDF drivers)")
	withDXSDK := fs.Bool("with-dxsdk", false, "also fetch and install the DirectX SDK (June 2010): real D3DX9/10/11, XInput and XAudio2 headers and import libs, dropped from the modern Windows SDK")
	var archsFlag stringList
	fs.Var(&archsFlag, "architecture", "target architecture to include (x86, x64, arm, arm64, host); repeatable")
	var ignoreFlag stringList
	fs.Var(&ignoreFlag, "ignore", "package id to skip; repeatable")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	packages := fs.Args()

	for _, a := range archsFlag {
		if !validArchitectures[a] {
			fmt.Fprintf(os.Stderr, "vintner download: invalid --architecture %q (expected one of x86, x64, arm, arm64, host)\n", a)
			return 2
		}
	}
	if *hostArch != "" && !validHostArchs[*hostArch] {
		fmt.Fprintf(os.Stderr, "vintner download: invalid --host-arch %q (expected one of x86, x64, arm64)\n", *hostArch)
		return 2
	}

	opts := &download.Options{
		Package:         packages,
		Ignore:          []string(ignoreFlag),
		Architecture:    []string(archsFlag),
		HostArch:        *hostArch,
		OnlyHost:        *onlyHost,
		MSVCVersion:     *msvcVersion,
		SDKVersion:      *sdkVersion,
		IncludeOptional: *includeOptional,
		SkipRecommended: *skipRecommended,
		Language:        *language,
		WithWDK:         *withWDK,
	}

	manifestURL := *manifestFile
	if manifestURL == "" {
		url, err := download.FetchChannelManifest(*major, *preview)
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
		manifestURL = url
	} else {
		manifestURL = "file:" + manifestURL
	}

	manifest, err := download.FetchInstallerManifest(manifestURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}

	if opts.HostArch == "" {
		opts.HostArch = detectHostArch()
	}
	fmt.Println(i18n.T("download.host_arch", opts.HostArch))

	idx := download.BuildIndex(manifest, opts.HostArch, opts.Language)

	if *listWorkloads || *listComponents {
		if *listWorkloads {
			printPackageList("download.workloads_header", download.PackagesByType(idx, "Workload"), opts.Language)
		}
		if *listComponents {
			printPackageList("download.components_header", download.PackagesByType(idx, "Component"), opts.Language)
		}
		return 0
	}

	if !*acceptLicense && !*printDepsTree {
		license := "the Visual Studio Build Tools license"
		if p := idx.Find("Microsoft.VisualStudio.Product.BuildTools", nil); p != nil && len(p.LocalizedResources) > 0 {
			license = p.LocalizedResources[0].License
		}
		if !promptAcceptLicense(license) {
			return 0
		}
	}

	if err := download.ResolveSelection(opts, idx); err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}

	if *printDepsTree {
		download.PrintDependencyTree(os.Stdout, idx, opts)
		return 0
	}

	selected, err := download.ExpandSelection(idx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}
	var downloadSize, installSize int64
	for _, p := range selected {
		downloadSize += p.DownloadSize()
		installSize += p.InstalledSize()
	}
	fmt.Print(i18n.T("download.selected",
		len(selected), download.HumanizeBytes(downloadSize), download.HumanizeBytes(installSize)))

	cache := *cacheDir
	removeCache := false
	if cache == "" {
		tmp, err := os.MkdirTemp("", "vintner-cache-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
		cache = tmp
		removeCache = true
	}
	if removeCache {
		defer os.RemoveAll(cache)
	}

	if !*onlyDownload && *dest == "" {
		def, err := defaultToolchainDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
		*dest = def
		fmt.Println(i18n.T("download.default_dest", *dest))
	}

	if err := download.FetchPayloads(selected, cache, *onlyDownload); err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}
	if *onlyDownload {
		return 0
	}

	destAbs, err := filepath.Abs(*dest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}
	unlock, err := lock.Acquire(destAbs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}
	defer unlock()

	unpack := destAbs
	if !*onlyUnpack {
		unpack = filepath.Join(destAbs, "unpack")
	}
	if err := download.UnpackSelectedPackages(selected, cache, unpack); err != nil {
		fmt.Fprintln(os.Stderr, "vintner download:", err)
		return 1
	}

	// Wine doesn't honor .exe.config <dependentAssembly> redirects, so copy
	// MSBuild's redirected assemblies next to it directly.
	for _, hostArch := range []string{"amd64", "arm64"} {
		msbuildExe := filepath.Join(unpack, "MSBuild", "Current", "Bin", hostArch, "MSBuild.exe")
		if err := download.CopyRedirectedAssemblies(msbuildExe); err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
	}

	if !*onlyUnpack {
		if err := download.RelocateBuildTools(unpack, destAbs); err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
		if !*keepUnpack {
			os.RemoveAll(unpack)
		}
		if !*skipPatch && *major == 18 {
			if err := download.ApplyCompatibilityFixes(destAbs); err != nil {
				fmt.Fprintln(os.Stderr, "vintner download:", err)
				return 1
			}
		}
	}

	if opts.WithWDK && !*onlyUnpack {
		if err := downloadWDK(opts, selected, cache, destAbs, *major); err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
	}

	if *withDXSDK && !*onlyUnpack {
		if err := downloadDXSDK(cache, destAbs); err != nil {
			fmt.Fprintln(os.Stderr, "vintner download:", err)
			return 1
		}
	}

	fmt.Println(i18n.T("download.done", destAbs))
	return 0
}

// downloadWDK fetches the WDK NuGet package(s) matching opts.Architecture
// (only x64 and arm64 have one - there's no WDK package for x86/arm
// targets) into destAbs/wdk/<arch>, preferring a version matching the
// Windows SDK actually selected. See wdk.go for why this is a separate
// download path from the rest of ExpandSelection/FetchPayloads/Unpack.
func downloadWDK(opts *download.Options, selected []*download.Package, cache, destAbs string, major int) error {
	sdkBuild := download.SDKBuildPrefix(selected)
	vsVersion := fmt.Sprintf("%d.0", major)
	var archs []string
	for _, a := range []string{"x64", "arm64"} {
		if contains(opts.Architecture, a) {
			archs = append(archs, a)
		}
	}
	if len(archs) == 0 {
		fmt.Println(i18n.T("download.wdk_skip"))
		return nil
	}
	for _, arch := range archs {
		version, err := download.FetchLatestWDKVersion(arch, sdkBuild)
		if err != nil {
			return err
		}
		wdkDir, err := download.DownloadWDK(arch, version, cache, destAbs, vsVersion)
		if err != nil {
			return err
		}
		fmt.Print(i18n.T("download.wdk_installed", arch, version, wdkDir))
	}
	return nil
}

// downloadDXSDK fetches and unpacks the DirectX SDK (June 2010) into
// destAbs/DXSDK. See internal/download/dxsdk.go for why this is a separate
// download path from the rest of ExpandSelection/FetchPayloads/Unpack.
func downloadDXSDK(cache, destAbs string) error {
	dxsdkDir, err := download.DownloadDXSDK(cache, destAbs)
	if err != nil {
		return err
	}
	fmt.Print(i18n.T("download.dxsdk_installed", dxsdkDir))
	return nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// printPackageList prints one line per package: its ID, and (when the
// manifest carries one) its human-readable title in the requested language.
// headerKey is an i18n catalog key taking the package count as its one arg.
func printPackageList(headerKey string, pkgs []*download.Package, language string) {
	fmt.Print(i18n.T(headerKey, len(pkgs)))
	for _, p := range pkgs {
		if lr := p.Localized(language); lr != nil && lr.Title != "" {
			fmt.Printf("  %-65s %s\n", p.ID, lr.Title)
		} else {
			fmt.Printf("  %s\n", p.ID)
		}
	}
}

var validArchitectures = map[string]bool{"x86": true, "x64": true, "arm": true, "arm64": true, "host": true}
var validHostArchs = map[string]bool{"x86": true, "x64": true, "arm64": true}

func detectHostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

func promptAcceptLicense(license string) bool {
	fmt.Print(i18n.T("download.license_prompt", license))
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "yes":
			return true
		case "no":
			return false
		}
		fmt.Print(i18n.T("download.license_reprompt"))
	}
	return false
}

// stringList implements flag.Value to collect a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
