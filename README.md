# msvc-go-wine

Cross compile with MSVC on Linux, using Wine — a single-binary Go tool
inspired by [mstorsjo/msvc-wine](https://github.com/mstorsjo/msvc-wine)'s
approach (download the real MSVC/WinSDK, wrap the compiler under Wine),
implemented independently.

Once installed, you invoke the real Microsoft toolchain exactly like on
Windows: `cl`, `link`, `lib`, `rc`, `midl`, `mt`, `dumpbin`, `msbuild`,
`nmake`, `ml`, `ml64`, `armasm`, `armasm64` all just work from your `PATH`.

## How it works

`msvc-go-wine` is one Go binary that behaves differently depending on the
name it's invoked as (a "multi-call binary", like busybox):

- Invoked as `cl`, `link`, `lib`, ... → it loads a small per-architecture
  `env.json`, builds the `INCLUDE`/`LIB`/`WINEPATH` environment Wine needs,
  rewrites absolute unix paths in the arguments into Wine's `z:\...` form
  (working around [a Wine/cl.exe include-path bug](https://bugs.winehq.org/show_bug.cgi?id=55200)),
  runs the real `.exe` under `wine`/`wine64`, and rewrites the tool's output
  back from `z:\...` paths to plain unix paths so your build system's error
  parsing keeps working.
- Invoked as `msvc-go-wine` → it exposes the `download`, `install`, `env` and
  `version` management subcommands described below.

## Quick start

```bash
# 1. Download and unpack MSVC + Windows SDK (requires accepting Microsoft's
#    Visual Studio Build Tools license, and msitools for unpacking .msi payloads)
msvc-go-wine download --accept-license --dest ~/my_msvc

# 2. Wire up the tool wrappers
msvc-go-wine install ~/my_msvc

# 3. Add the toolchain to PATH and build
export PATH=~/my_msvc/bin/x64:$PATH
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
msvc-go-wine download --dest <dir> [options]   fetch and unpack MSVC/WinSDK
msvc-go-wine install <dir>                     wire up wrappers for a downloaded MSVC
msvc-go-wine env --bin <dir>/bin/<arch>        print INCLUDE/LIB for native clang-cl/lld-link use
msvc-go-wine version                           print the version
```

`download` supports `--msvc-version`, `--sdk-version`, `--architecture`,
`--host-arch`, `--with-*` component toggles, `--ignore`, `--only-download`,
`--only-unpack`, `--keep-unpack`, `--cache`, `--language`,
`--include-optional`, `--skip-recommended`, `--major`, `--preview`,
`--manifest`. Run `msvc-go-wine download -h` for the full list.

### Using clang-cl/lld-link instead of Wine

You don't need Wine at all if you drive the (nonredistributable) MSVC/WinSDK
headers and libraries with Clang/LLD in MSVC-compatible mode:

```bash
eval "$(msvc-go-wine env --bin ~/my_msvc/bin/x64)"
clang-cl -c hello.c
lld-link hello.obj -out:hello.exe
```

## Building from source

```bash
go build -o msvc-go-wine ./cmd/msvc-go-wine
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

- `download` doesn't yet support printing the dependency/reverse-dependency
  tree, listing available workloads/components/packages, or installing the
  Windows Driver Kit via `--with-wdk-installers`; the core selection/
  download/unpack/install pipeline is fully implemented.

## License

MIT, see [LICENSE.txt](LICENSE.txt) - covers msvc-go-wine's own source only.
The MSVC Build Tools / Windows SDK that `download` fetches remain governed
by Microsoft's own license (accepted via `--accept-license`), same as with
any other way of obtaining them.
