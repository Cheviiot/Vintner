package main

import "fmt"

// runCompletion prints a shell completion script for shell ("bash" or
// "zsh") to stdout, meant to be sourced directly:
//
//	source <(vintner completion bash)   # or add to ~/.bashrc
//	source <(vintner completion zsh)    # or add to ~/.zshrc
//
// The flag lists below are hand-maintained alongside download.go/env.go's
// flag.FlagSet definitions rather than generated from them - there's no
// reflection-friendly registry to walk, and the flag set rarely changes.
func runCompletion(args []string) int {
	if len(args) != 1 {
		fmt.Println("usage: vintner completion bash|zsh")
		return 1
	}
	switch args[0] {
	case "bash":
		fmt.Print(bashCompletionScript)
		return 0
	case "zsh":
		fmt.Print(zshCompletionScript)
		return 0
	default:
		fmt.Printf("vintner completion: unsupported shell %q (want bash or zsh)\n", args[0])
		return 1
	}
}

const downloadFlags = "--dest --cache --major --preview --manifest --accept-license " +
	"--msvc-version --sdk-version --host-arch --only-host --language " +
	"--include-optional --skip-recommended --only-download --only-unpack " +
	"--keep-unpack --skip-patch --list-workloads --list-components " +
	"--print-deps-tree --with-wdk --architecture --ignore -h --help"

var bashCompletionScript = `# vintner bash completion - eval "$(vintner completion bash)"
_vintner_complete() {
    local cur cmd
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    cmd="${COMP_WORDS[1]}"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=($(compgen -W "download dl install i env e version v help h completion" -- "$cur"))
        return 0
    fi

    case "$cmd" in
        download|dl)
            COMPREPLY=($(compgen -W "` + downloadFlags + `" -- "$cur"))
            ;;
        install|i)
            COMPREPLY=($(compgen -d -- "$cur"))
            ;;
        env|e)
            COMPREPLY=($(compgen -W "--bin -h --help" -- "$cur"))
            ;;
        completion)
            COMPREPLY=($(compgen -W "bash zsh" -- "$cur"))
            ;;
    esac
    return 0
}
complete -F _vintner_complete vintner
`

var zshCompletionScript = `#compdef vintner
# vintner zsh completion - source <(vintner completion zsh)

_vintner() {
    local -a subcommands
    subcommands=(
        'download:fetch and unpack MSVC/WinSDK/WDK'
        'dl:alias for download'
        'install:wire up wrappers for a downloaded MSVC'
        'i:alias for install'
        'env:print INCLUDE/LIB for native clang-cl/lld-link use'
        'e:alias for env'
        'version:print the version'
        'v:alias for version'
        'help:print usage'
        'h:alias for help'
        'completion:print a shell completion script'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' subcommands
        return
    fi

    case "${words[2]}" in
        download|dl)
            local -a flags
            flags=(
                '--dest[directory to install into]:directory:_files -/'
                '--cache[persistent download cache directory]:directory:_files -/'
                '--major[major VS version]:version:'
                '--preview[use the preview/insiders channel]'
                '--manifest[use a predownloaded installer manifest file]:file:_files'
                '--accept-license[do not prompt for accepting the license]'
                '--msvc-version[install a specific MSVC toolchain version]:version:'
                '--sdk-version[install a specific Windows SDK version]:version:'
                '--host-arch[host architecture]:arch:(x86 x64 arm64)'
                '--only-host[only download packages matching the host architecture]'
                '--language[preferred package language]:language:'
                '--include-optional[include all optional dependencies]'
                '--skip-recommended[skip recommended dependencies]'
                '--only-download[stop after downloading package files]'
                '--only-unpack[unpack without pruning to just the CLI tools]'
                '--keep-unpack[keep the scratch unpack dir]'
                '--skip-patch[do not apply the Wine compatibility patches]'
                '--list-workloads[list available workloads and exit]'
                '--list-components[list available components and exit]'
                '--print-deps-tree[print the dependency tree and exit]'
                '--with-wdk[also fetch the Windows Driver Kit]'
                '--architecture[target architecture]:arch:(x86 x64 arm arm64 host)'
                '--ignore[package id to skip]:package id:'
                '-h[show help]'
                '--help[show help]'
            )
            _arguments $flags
            ;;
        install|i)
            _files -/
            ;;
        env|e)
            _arguments \
                '--bin[bin/<arch> directory produced by install]:directory:_files -/' \
                '-h[show help]' '--help[show help]'
            ;;
        completion)
            _values 'shell' bash zsh
            ;;
    esac
}

_vintner "$@"
`
