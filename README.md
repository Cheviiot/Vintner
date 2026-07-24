# vintner

Cross compile with MSVC on Linux, using Wine — a single-binary Go tool
inspired by [mstorsjo/msvc-wine](https://github.com/mstorsjo/msvc-wine)'s
approach (download the real MSVC/WinSDK, wrap the compiler under Wine),
implemented independently.

Once installed, you invoke the real Microsoft toolchain exactly like on
Windows: `cl`, `link`, `lib`, `rc`, `midl`, `mc`, `mt`, `dumpbin`, `msbuild`,
`nmake`, `ml`, `ml64`, `armasm`, `armasm64`, plus trivial `cmd`/`findstr`
shims, all just work from your `PATH`.

## How it works

`vintner` is one Go binary that behaves differently depending on the name
it's invoked as (a "multi-call binary", like busybox):

- Invoked as `cl`, `link`, `lib`, ... → it loads a small per-architecture
  `env.json`, builds the `INCLUDE`/`LIB`/`WINEPATH` environment Wine needs,
  rewrites absolute unix paths in the arguments into Wine's `z:\...` form
  (working around [a Wine/cl.exe include-path bug](https://bugs.winehq.org/show_bug.cgi?id=55200)),
  runs the real `.exe` under `wine`/`wine64`, and rewrites the tool's output
  back from `z:\...` paths to plain unix paths so your build system's error
  parsing keeps working.
- Invoked as `vintner` → it exposes the `download`, `install`, `env` and
  `version` management subcommands described below (each also has a short
  alias: `dl`, `i`, `e`, `v`; `help`/`h` prints usage).

## Quick start

```bash
# 1. Download and unpack MSVC + Windows SDK into ~/.vintner (requires
#    accepting Microsoft's Visual Studio Build Tools license, and msitools
#    for unpacking .msi payloads). Pass --dest <dir> for a different location.
vintner download --accept-license

# 2. Wire up the tool wrappers
vintner install

# 3. Add the toolchain to PATH and build
export PATH=~/.vintner/bin/x64:$PATH
cl /nologo /EHsc hello.cpp
```

### Prerequisites

- `wine` (or `wine64`) — runs the real `cl.exe`/`link.exe`/etc.
- `msitools` (`msiextract`) — unpacks the `.msi` payloads MSVC/WinSDK ship as.
- `git` — used to apply the small compatibility patches bundled with
  `download` (see Compatibility patches below).

On ALT Linux:

```bash
pkcon install wine msitools
```

## Commands

```
vintner download (dl) --accept-license [--dest <dir>] [options]   fetch and unpack MSVC/WinSDK/WDK
vintner install (i) [dir]                                         wire up wrappers for a downloaded MSVC
vintner env (e) --bin <dir>/bin/<arch>                            print INCLUDE/LIB for native clang-cl/lld-link use
vintner version (v)                                               print the version
vintner help (h)                                                  print usage
```

`--dest`/`[dir]` both default to `~/.vintner` when omitted.

`download`'s main options: `--msvc-version`, `--sdk-version`,
`--architecture`, `--host-arch`, `--only-host`, `--with-wdk` (also fetch the
Windows Driver Kit, for building KMDF/UMDF drivers), `--ignore`,
`--only-download`, `--only-unpack`, `--keep-unpack`, `--skip-patch`,
`--cache`, `--language`, `--include-optional`, `--skip-recommended`,
`--major`, `--preview`, `--manifest`, `--list-workloads`,
`--list-components`, `--print-deps-tree`. Run `vintner download -h` for the
full list with descriptions.

`--list-workloads`/`--list-components` print every workload/component id
(with its human-readable title) available in the fetched manifest and exit
without downloading anything - useful for discovering what to pass as a bare
package id or via `--with-*`. `--print-deps-tree` prints the dependency tree
of whatever would actually be selected (honoring every other flag), also
without downloading.

### Building drivers (WDK)

`vintner download --with-wdk` additionally fetches the Windows Driver Kit
(headers, import libs, and the MSBuild `WindowsKernelModeDriver10.0`/
`WindowsUserModeDriver10.0` PlatformToolsets) so `msbuild` can build real
KMDF/UMDF drivers - compiling, linking, INF stamping and the `Inf2Cat`
signability check (with `SignMode=off`) all work under Wine. Verified
end-to-end against a real sample driver from
[microsoft/Windows-driver-samples](https://github.com/microsoft/Windows-driver-samples).
Only x64 and arm64 targets have a WDK package upstream (no x86/arm).

### Language

CLI messages (usage text, progress lines, prompts) are in English by
default. Set `VINTNER_LANG=ru` (or have a `ru`-prefixed `LC_ALL`/
`LC_MESSAGES`/`LANG`, e.g. `ru_RU.UTF-8`) for Russian. Deeper error text
bubbled up from internal packages stays in English.

### Using clang-cl/lld-link instead of Wine

You don't need Wine at all if you drive the (nonredistributable) MSVC/WinSDK
headers and libraries with Clang/LLD in MSVC-compatible mode:

```bash
eval "$(vintner env --bin ~/.vintner/bin/x64)"
clang-cl -c hello.c
lld-link hello.obj -out:hello.exe
```

## Building from source

```bash
go build -o vintner ./cmd/vintner
```

Go 1.23+ is all you need to build it; `wine`/`msitools` are only needed at
run time (`install`/tool invocation and `download` respectively).

## toolrelay.exe

`install` compiles `assets/vendor/toolrelay.cpp` (a small native Windows
launcher, original to this project) with the freshly-installed host-arch
`cl.exe` (best-effort: if `wine` isn't present yet, or the compile fails,
install still succeeds and the wrapper runtime just falls back to invoking
tools directly through wine). When present, every non-MSBuild tool
invocation is routed through it via two named FIFOs. This is what lets
`mt.exe`'s CMake-compatibility exit-code translation (`0x41020001` → `0xbb`)
survive Wine's own exit-code truncation: only a native Windows process
observing the untranslated code via `GetExitCodeProcess()` can catch it
before Wine marshals the process exit back to Unix and drops everything but
the low byte.

## Compatibility patches

`download` applies a handful of small patches (`assets/patches`) to the
downloaded MSVC/WinSDK tree - independently written for this project - that
make `VsDevCmd.bat` and MSBuild's SDK-detection props work without a
Windows Registry (which doesn't exist under Wine): they check the SDK
directly under the VS install root instead of querying the registry, skip
telemetry, and don't hard-fail devcmd setup when an optional component
(ConnectionManagerExe, bundled CMake/Ninja) wasn't downloaded.

## Known gaps

None currently tracked. Download/select/unpack/install, general MSBuild
projects, WDK driver builds, dependency-tree printing, and
workload/component listing are all implemented and verified against real
projects.

## License

MIT, see [LICENSE.txt](LICENSE.txt) - covers vintner's own source only. The
MSVC Build Tools / Windows SDK / WDK that `download` fetches remain governed
by Microsoft's own license (accepted via `--accept-license`), same as with
any other way of obtaining them.
