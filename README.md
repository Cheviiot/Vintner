# vintner

[![CI](https://github.com/Cheviiot/vintner/actions/workflows/ci.yml/badge.svg)](https://github.com/Cheviiot/vintner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Cheviiot/vintner)](https://github.com/Cheviiot/vintner/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE.txt)

**vintner** cross compiles with the real MSVC toolchain on Linux, using Wine —
a single Go binary, inspired by [mstorsjo/msvc-wine](https://github.com/mstorsjo/msvc-wine)'s
approach (download the actual MSVC/WinSDK, wrap the compiler under Wine) and
implemented independently.

Once installed, you invoke the real Microsoft toolchain exactly like on
Windows: `cl`, `link`, `lib`, `rc`, `midl`, `mc`, `mt`, `dumpbin`, `msbuild`,
`nmake`, `ml`, `ml64`, `armasm`, `armasm64`, plus trivial `cmd`/`findstr`
shims, all work from your `PATH` — including full MSBuild projects and, with
`--with-wdk`, real KMDF/UMDF Windows drivers.

## Contents

- [How it works](#how-it-works)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Commands](#commands)
- [Building drivers (WDK)](#building-drivers-wdk)
- [Language](#language)
- [Shell completion](#shell-completion)
- [Using clang-cl/lld-link instead of Wine](#using-clang-cllld-link-instead-of-wine)
- [Building from source](#building-from-source)
- [How the pieces fit together](#how-the-pieces-fit-together)
- [License](#license)

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
  `version` management subcommands described below (short aliases: `dl`,
  `i`, `e`, `v`; `help`/`h` prints usage).

## Installation

**On ALT Linux, via [Nivora](https://github.com/Cheviiot/Nivora):**

```bash
stplr install nivora/vintner
```

**Prebuilt binary**, from the [latest release](https://github.com/Cheviiot/vintner/releases/latest):

```bash
curl -fLo vintner "https://github.com/Cheviiot/vintner/releases/latest/download/vintner-linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x vintner
sudo install vintner /usr/local/bin/vintner
```

**From source** — see [Building from source](#building-from-source).

Either way, `wine`/`wine64`, `msitools` (for `msiextract`) and `git` need to
be on `PATH` at run time (see [Prerequisites](#prerequisites) below); Nivora
installs already pull these in as package dependencies.

### Prerequisites

- `wine` (or `wine64`) — runs the real `cl.exe`/`link.exe`/etc.
- `msitools` (`msiextract`) — unpacks the `.msi` payloads MSVC/WinSDK ship as.
- `git` — used to apply the small compatibility patches bundled with
  `download` (see [Compatibility patches](#compatibility-patches) below).

On ALT Linux:

```bash
pkcon install wine msitools git
```

## Quick start

```bash
# 1. Download and unpack MSVC + Windows SDK into ~/.vintner (requires
#    accepting Microsoft's Visual Studio Build Tools license). Pass
#    --dest <dir> for a different location.
vintner download --accept-license

# 2. Wire up the tool wrappers
vintner install

# 3. Add the toolchain to PATH and build
export PATH=~/.vintner/bin/x64:$PATH
cl /nologo /EHsc hello.cpp
```

## Commands

```
vintner download (dl) --accept-license [--dest <dir>] [options]   fetch and unpack MSVC/WinSDK/WDK
vintner install (i) [dir]                                         wire up wrappers for a downloaded MSVC
vintner env (e) --bin <dir>/bin/<arch>                            print INCLUDE/LIB for native clang-cl/lld-link use
vintner version (v)                                               print the version
vintner help (h)                                                  print usage
vintner completion bash|zsh                                       print a shell completion script
```

`--dest`/`[dir]` both default to `~/.vintner` when omitted.

`download`'s main options: `--msvc-version`, `--sdk-version`,
`--architecture` (repeatable: `x86`/`x64`/`arm`/`arm64`/`host`),
`--host-arch`, `--only-host`, `--with-wdk` (see below), `--ignore`
(repeatable), `--only-download`, `--only-unpack`, `--keep-unpack`,
`--skip-patch`, `--cache`, `--language`, `--include-optional`,
`--skip-recommended`, `--major`, `--preview`, `--manifest`,
`--list-workloads`, `--list-components`, `--print-deps-tree`. Run
`vintner download -h` for the full list with descriptions.

`--list-workloads`/`--list-components` print every workload/component id
(with its human-readable title) available in the fetched manifest and exit
without downloading anything — useful for discovering what to pass as a bare
package id or via `--with-*`. `--print-deps-tree` prints the dependency tree
of whatever would actually be selected (honoring every other flag), also
without downloading.

## Building drivers (WDK)

```bash
vintner download --accept-license --with-wdk
```

additionally fetches the Windows Driver Kit (headers, import libs, and the
MSBuild `WindowsKernelModeDriver10.0`/`WindowsUserModeDriver10.0`
PlatformToolsets) so `msbuild` can build real KMDF/UMDF drivers —
compiling, linking, INF stamping and the `Inf2Cat` signability check (with
`SignMode=off`) all work under Wine. Verified end-to-end against a real
sample driver from
[microsoft/Windows-driver-samples](https://github.com/microsoft/Windows-driver-samples).
Only x64 and arm64 targets have a WDK package upstream (no x86/arm).

## Language

CLI messages (usage text, progress lines, prompts) default to English. Set
`VINTNER_LANG=ru` (or have a `ru`-prefixed `LC_ALL`/`LC_MESSAGES`/`LANG`,
e.g. `ru_RU.UTF-8`) for Russian:

```bash
VINTNER_LANG=ru vintner help
```

Deeper error text bubbled up from internal packages stays in English.

## Shell completion

```bash
source <(vintner completion bash)   # or add to ~/.bashrc
source <(vintner completion zsh)    # or add to ~/.zshrc
```

Completes subcommands (including the short aliases), `download`'s flags,
and directory arguments for `install`/`env --bin`.

## Using clang-cl/lld-link instead of Wine

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

```bash
go vet ./...
go test ./...
```

## How the pieces fit together

<details>
<summary><strong>toolrelay.exe</strong> — surviving Wine's exit-code truncation</summary>

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

</details>

<details>
<summary><strong>Compatibility patches</strong> — making MSBuild work without a Windows Registry</summary>

`download` applies a handful of small patches (`assets/patches`) to the
downloaded MSVC/WinSDK tree — independently written for this project — that
make `VsDevCmd.bat` and MSBuild's SDK-detection props work without a
Windows Registry (which doesn't exist under Wine): they check the SDK
directly under the VS install root instead of querying the registry, skip
telemetry, and don't hard-fail devcmd setup when an optional component
(ConnectionManagerExe, bundled CMake/Ninja) wasn't downloaded.

</details>

## License

MIT, see [LICENSE.txt](LICENSE.txt) — covers vintner's own source only. The
MSVC Build Tools / Windows SDK / WDK that `download` fetches remain governed
by Microsoft's own license (accepted via `--accept-license`), same as with
any other way of obtaining them.
