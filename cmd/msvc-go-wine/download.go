package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Cheviiot/msvc-go-wine/internal/download"
)

func runDownload(args []string) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	dest := fs.String("dest", "", "directory to install into (required unless --only-download)")
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
	var archsFlag stringList
	fs.Var(&archsFlag, "architecture", "target architecture to include (x86, x64, arm, arm64, host); repeatable")
	var ignoreFlag stringList
	fs.Var(&ignoreFlag, "ignore", "package id to skip; repeatable")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	packages := fs.Args()

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
	}

	manifestURL := *manifestFile
	if manifestURL == "" {
		url, err := download.FetchChannelManifest(*major, *preview)
		if err != nil {
			fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
			return 1
		}
		manifestURL = url
	} else {
		manifestURL = "file:" + manifestURL
	}

	manifest, err := download.FetchInstallerManifest(manifestURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}

	if opts.HostArch == "" {
		opts.HostArch = detectHostArch()
	}
	fmt.Println("Install packages for", opts.HostArch, "host architecture")

	idx := download.BuildIndex(manifest, opts.HostArch, opts.Language)

	if !*acceptLicense {
		license := "the Visual Studio Build Tools license"
		if p := idx.Find("Microsoft.VisualStudio.Product.BuildTools", nil); p != nil && len(p.LocalizedResources) > 0 {
			license = p.LocalizedResources[0].License
		}
		if !promptAcceptLicense(license) {
			return 0
		}
	}

	if err := download.ResolveSelection(opts, idx); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}

	selected, err := download.ExpandSelection(idx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}
	var downloadSize, installSize int64
	for _, p := range selected {
		downloadSize += p.DownloadSize()
		installSize += p.InstalledSize()
	}
	fmt.Printf("Selected %d packages, for a total download size of %s, install size of %s\n",
		len(selected), download.HumanizeBytes(downloadSize), download.HumanizeBytes(installSize))

	cache := *cacheDir
	removeCache := false
	if cache == "" {
		tmp, err := os.MkdirTemp("", "msvc-go-wine-cache-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
			return 1
		}
		cache = tmp
		removeCache = true
	}
	if removeCache {
		defer os.RemoveAll(cache)
	}

	if !*onlyDownload && *dest == "" {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download: --dest is required unless --only-download is set")
		return 1
	}

	if err := download.FetchPayloads(selected, cache, *onlyDownload); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}
	if *onlyDownload {
		return 0
	}

	destAbs, err := filepath.Abs(*dest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}

	unpack := destAbs
	if !*onlyUnpack {
		unpack = filepath.Join(destAbs, "unpack")
	}
	if err := download.UnpackSelectedPackages(selected, cache, unpack); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
		return 1
	}

	// Wine doesn't honor .exe.config <dependentAssembly> redirects, so copy
	// MSBuild's redirected assemblies next to it directly.
	for _, hostArch := range []string{"amd64", "arm64"} {
		msbuildExe := filepath.Join(unpack, "MSBuild", "Current", "Bin", hostArch, "MSBuild.exe")
		if err := download.CopyRedirectedAssemblies(msbuildExe); err != nil {
			fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
			return 1
		}
	}

	if !*onlyUnpack {
		if err := download.RelocateBuildTools(unpack, destAbs); err != nil {
			fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
			return 1
		}
		if !*keepUnpack {
			os.RemoveAll(unpack)
		}
		if !*skipPatch && *major == 18 {
			if err := download.ApplyCompatibilityFixes(destAbs); err != nil {
				fmt.Fprintln(os.Stderr, "msvc-go-wine download:", err)
				return 1
			}
		}
	}

	fmt.Println("Done. Next: msvc-go-wine install", destAbs)
	return 0
}

func detectHostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

func promptAcceptLicense(license string) bool {
	fmt.Printf("Do you accept the license at %s (yes/no)? ", license)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "yes":
			return true
		case "no":
			return false
		}
		fmt.Print("Do you accept the license? Answer \"yes\" or \"no\": ")
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
