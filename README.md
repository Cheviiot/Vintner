# vintner

[![CI](https://github.com/Cheviiot/vintner/actions/workflows/ci.yml/badge.svg)](https://github.com/Cheviiot/vintner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Cheviiot/vintner)](https://github.com/Cheviiot/vintner/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE.txt)

vintner cross-compiles with the real MSVC toolchain on Linux, using Wine.
One Go binary drops in as `cl`, `link`, `lib`, `rc`, `midl`, `mc`, `mt`,
`dumpbin`, `msbuild`, `nmake`, `ml`, `ml64`, `armasm`, `armasm64`, plus
`cmd`/`findstr` shims, so once installed you invoke the real Microsoft
tools exactly like on Windows. It handles full MSBuild projects, with
`--with-wdk` real KMDF/UMDF Windows drivers, and with `--with-dxsdk` the
real D3DX9 headers/libs.

Inspired by [mstorsjo/msvc-wine](https://github.com/mstorsjo/msvc-wine)'s
approach: download the real MSVC/WinSDK, wrap the compiler under Wine.

## Contents

- [How it works](#how-it-works)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Commands](#commands)
- [Building drivers (WDK)](#building-drivers-wdk)
- [Building against D3DX9 (DirectX SDK)](#building-against-d3dx9-directx-sdk)
- [Automated/scripted builds](#automatedscripted-builds)
- [Language](#language)
- [Shell completion](#shell-completion)
- [Using clang-cl/lld-link instead of Wine](#using-clang-cllld-link-instead-of-wine)
- [toolrelay.exe](#toolrelayexe)
- [Compatibility patches](#compatibility-patches)
- [Building from source](#building-from-source)
- [License](#license)

## How it works

vintner is a multi-call binary, like busybox: it behaves differently
depending on the name it's invoked as.

- As `cl`, `link`, `lib`, and the rest: it loads a per-architecture
  `env.json`, sets `INCLUDE`/`LIB`/`WINEPATH`, and rewrites absolute Unix
  paths in the arguments to Wine's `z:\...` form (Wine and cl.exe
  otherwise mishandle relative includes — see
  [winehq bug 55200](https://bugs.winehq.org/show_bug.cgi?id=55200)). It
  then runs the real `.exe` under `wine`/`wine64`, and rewrites `z:\...`
  paths back to Unix paths in the output, so your build system's error
  parsing keeps working.
- As `vintner`: it exposes the `download`, `install`, `env`, `version`
  and `completion` subcommands below (short aliases: `dl`, `i`, `e`, `v`;
  `help`/`h` prints usage).

## Installation

On ALT Linux, via [Nivora](https://github.com/Cheviiot/Nivora):

```bash
stplr install nivora/vintner
```

Prebuilt binary, from the [latest release](https://github.com/Cheviiot/vintner/releases/latest):

```bash
curl -fLo vintner "https://github.com/Cheviiot/vintner/releases/latest/download/vintner-linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x vintner
sudo install vintner /usr/local/bin/vintner
```

From source: see [Building from source](#building-from-source).

Either way, `wine`/`wine64`, `msitools` (for `msiextract`) and `git` need
to be on `PATH` at run time. Nivora installs pull these in automatically
as package dependencies.

### Prerequisites

- `wine` (or `wine64`) — runs the real `cl.exe`/`link.exe`/etc.
- `msitools` (`msiextract`) — unpacks the `.msi` payloads MSVC/WinSDK ship as.
- `git` — applies the compatibility patches bundled with `download` (see
  [Compatibility patches](#compatibility-patches)).
- `cabextract` — only needed for `download --with-dxsdk` (see
  [Building against D3DX9](#building-against-d3dx9-directx-sdk)).

On ALT Linux:

```bash
pkcon install wine msitools git cabextract
```

## Quick start

```bash
# 1. Download and unpack MSVC + Windows SDK into ~/.vintner (accepts
#    Microsoft's Visual Studio Build Tools license). Pass --dest <dir>
#    for a different location.
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
`--host-arch`, `--only-host`, `--with-wdk` (see below), `--with-dxsdk`
(see below), `--ignore` (repeatable), `--only-download`, `--only-unpack`,
`--keep-unpack`, `--skip-patch`, `--cache`, `--language`,
`--include-optional`, `--skip-recommended`, `--major`, `--preview`,
`--manifest`, `--list-workloads`, `--list-components`,
`--print-deps-tree`. Run `vintner download -h` for the full list with
descriptions.

`--list-workloads`/`--list-components` print every workload/component id
and its human-readable title from the fetched manifest, then exit
without downloading anything. Useful for finding what to pass as a bare
package id or through `--with-*`. `--print-deps-tree` prints the
dependency tree of whatever would actually be selected — honoring every
other flag — without downloading anything.

## Building drivers (WDK)

`--with-wdk` also fetches the Windows Driver Kit: headers, import libs,
and the MSBuild `WindowsKernelModeDriver10.0`/`WindowsUserModeDriver10.0`
PlatformToolsets.

```bash
vintner download --accept-license --with-wdk
```

With it, `msbuild` builds real KMDF/UMDF drivers — compiling, linking,
INF stamping, and the `Inf2Cat` signability check (`SignMode=off`) all
work under Wine. Tested against a real sample driver from
[microsoft/Windows-driver-samples](https://github.com/microsoft/Windows-driver-samples).
Only x64 and arm64 targets have a WDK package upstream; there's no x86 or
arm one.

## Building against D3DX9 (DirectX SDK)

`--with-dxsdk` fetches the DirectX SDK (June 2010) — the last standalone
release of D3DX9/10/11, XInput and XAudio2, dropped from the Windows SDK
entirely once D3DX was deprecated. It unpacks the real headers and x86/x64
import libs (`d3dx9.h`/`d3dx9.lib` included) to `<dest>/DXSDK`.

```bash
vintner download --accept-license --with-dxsdk
```

Point your project's `IncludePath`/`LibraryPath` at
`<dest>/DXSDK/Include` and `<dest>/DXSDK/Lib/x86` or `<dest>/DXSDK/Lib/x64`.
Requires `cabextract` on `PATH` (the installer is a self-extracting CAB
archive).

## Automated/scripted builds

Every tool invocation runs unbounded by default, same as the real thing on
Windows. Set `VINTNER_TIMEOUT` (a `time.ParseDuration` string, e.g. `30m`,
`2h`) to have vintner kill and fail a build that runs longer than that
instead of hanging forever - meant for CI and other unattended callers, not
interactive use. This guards against one confirmed failure mode: an
MSBuild node-reuse worker (`/nodeReuse:true` is MSBuild's own default) can
survive its parent process under Wine and, if a prior build was
interrupted mid-compile, come back wedged - reused by the next `msbuild`
call and failing every subsequent build with a confusing, unrelated-looking
error, indefinitely, until it's killed by hand. vintner already forces
`/nodeReuse:false` on every `msbuild` invocation to prevent this in the
first place; `VINTNER_TIMEOUT` is the backstop for whatever else might
wedge under Wine that isn't MSBuild-specific.

```bash
VINTNER_TIMEOUT=30m msbuild MyProject.sln
```

## Language

CLI text (usage, progress lines, prompts) defaults to English. Set
`VINTNER_LANG=ru` (or a `ru`-prefixed `LC_ALL`/`LC_MESSAGES`/`LANG`, e.g.
`ru_RU.UTF-8`) for Russian:

```bash
VINTNER_LANG=ru vintner help
```

Error text from internal packages stays in English regardless.

## Shell completion

Already set up if you installed via Nivora. Otherwise:

```bash
source <(vintner completion bash)   # or add to ~/.bashrc
source <(vintner completion zsh)    # or add to ~/.zshrc
```

Completes subcommands, including the short aliases, `download`'s flags,
and directory arguments for `install`/`env --bin`.

## Using clang-cl/lld-link instead of Wine

The MSVC/WinSDK headers and libraries work directly with Clang/LLD in
MSVC-compatible mode. No Wine needed:

```bash
eval "$(vintner env --bin ~/.vintner/bin/x64)"
clang-cl -c hello.c
lld-link hello.obj -out:hello.exe
```

## toolrelay.exe

`install` compiles `assets/vendor/toolrelay.cpp`, a small native Windows
launcher, with the freshly-installed host-arch `cl.exe`. This is
best-effort: if `wine` isn't available yet, or the compile fails, install
still succeeds, and tool invocations just skip it. When present, every
non-MSBuild tool call is routed through it via two named FIFOs.

That's what lets `mt.exe`'s CMake-compatibility exit code
(`0x41020001` → `0xbb`) survive Wine's own exit-code truncation: a native
Windows process can read the real 32-bit exit code via
`GetExitCodeProcess()` before Wine collapses it to a single byte on the
way back to Unix.

## Compatibility patches

`download` applies a few small patches (`assets/patches`) to the
downloaded MSVC/WinSDK tree, so `VsDevCmd.bat` and MSBuild's
SDK-detection props work without a Windows Registry, which doesn't exist
under Wine. They look up the SDK directly under the VS install root
instead of querying the registry, skip telemetry, and don't fail devcmd
setup when an optional component (ConnectionManagerExe, bundled
CMake/Ninja) is missing.

## Building from source

```bash
go build -o vintner ./cmd/vintner
go vet ./...
go test ./...
```

Go 1.23+ builds it. `wine`/`msitools` are only needed at run time, for
`install`/tool invocation and `download` respectively.

## License

MIT (see [LICENSE.txt](LICENSE.txt)) for vintner's own source. The MSVC
Build Tools, Windows SDK, and WDK that `download` fetches stay under
Microsoft's own license (accepted via `--accept-license`), same as with
any other way of obtaining them.
