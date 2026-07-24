// Package i18n provides minimal message localization for msvc-go-wine's own
// CLI chrome (usage text, progress lines, prompts). It does not localize
// error messages bubbled up from deeper packages - those stay in English,
// matching how most cross-platform dev tools keep diagnostic text technical
// regardless of UI language.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Lang string

const (
	EN Lang = "en"
	RU Lang = "ru"
)

var current = detect()

// detect picks the active language from MSVC_GO_WINE_LANG (checked first, so
// it always overrides the locale), falling back to the POSIX locale
// variables in their usual priority order (LC_ALL, LC_MESSAGES, LANG). Any
// value starting with "ru" (case-insensitive) selects Russian; anything else
// falls back to English.
func detect() Lang {
	for _, key := range []string{"MSVC_GO_WINE_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "ru") {
			return RU
		}
		return EN
	}
	return EN
}

// Current returns the active language, for callers that need to branch
// beyond a simple T() lookup.
func Current() Lang { return current }

// T looks up key in the message catalog and formats it (via fmt.Sprintf)
// with args, if any. Keys with no translation for the current language fall
// back to English; keys missing from the catalog entirely are returned
// as-is, so a missing translation degrades to a visible-but-harmless string
// rather than a panic.
func T(key string, args ...any) string {
	msg := key
	if entry, ok := catalog[key]; ok {
		if m, ok := entry[current]; ok {
			msg = m
		} else {
			msg = entry[EN]
		}
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

var catalog = map[string]map[Lang]string{
	"main.usage": {
		EN: `msvc-go-wine - cross compile with MSVC on Linux via Wine

Usage:
  msvc-go-wine download (dl) --accept-license [--dest <dir>] [options]
                                                  fetch and unpack MSVC/WinSDK/WDK
  msvc-go-wine install (i) [dir]                 wire up wrappers for a downloaded MSVC
  msvc-go-wine env (e) --bin <dir/bin/arch>       print INCLUDE/LIB for native clang-cl/lld-link use
  msvc-go-wine version (v)                       print the version
  msvc-go-wine help (h)                          show this message

Run "msvc-go-wine <command> --help" for that command's own options - download
has many, including --with-wdk, --list-workloads, --list-components and
--print-deps-tree.

--dest/[dir] default to ~/.msvc-go-wine if omitted.
Language: set MSVC_GO_WINE_LANG=ru (or LANG=ru_RU...) for Russian output.

Once installed, add <dir>/bin/<arch> to PATH and invoke the tools directly:
  cl, link, lib, ml, ml64, mc, midl, mt, rc, dumpbin, msbuild, nmake, armasm, armasm64, cmd, findstr
`,
		RU: `msvc-go-wine — кросс-компиляция настоящим MSVC на Linux через Wine

Использование:
  msvc-go-wine download (dl) --accept-license [--dest <каталог>] [опции]
                                                  скачать и распаковать MSVC/WinSDK/WDK
  msvc-go-wine install (i) [каталог]             настроить обёртки для скачанного MSVC
  msvc-go-wine env (e) --bin <dir/bin/arch>      вывести INCLUDE/LIB для clang-cl/lld-link напрямую
  msvc-go-wine version (v)                       показать версию
  msvc-go-wine help (h)                          показать эту справку

Запустите «msvc-go-wine <команда> --help» для параметров конкретной команды —
у download их много, включая --with-wdk, --list-workloads, --list-components
и --print-deps-tree.

--dest/[каталог] по умолчанию — ~/.msvc-go-wine.
Язык: установите MSVC_GO_WINE_LANG=en (или LANG=en_US...) для вывода на английском.

После установки добавьте <dir>/bin/<arch> в PATH и вызывайте инструменты напрямую:
  cl, link, lib, ml, ml64, mc, midl, mt, rc, dumpbin, msbuild, nmake, armasm, armasm64, cmd, findstr
`,
	},
	"main.unknown_subcommand": {
		EN: "msvc-go-wine: unknown subcommand %q\n\n",
		RU: "msvc-go-wine: неизвестная подкоманда %q\n\n",
	},

	"install.usage": {
		EN: "usage: msvc-go-wine install (i) [dest]  (default: ~/.msvc-go-wine)",
		RU: "использование: msvc-go-wine install (i) [каталог]  (по умолчанию: ~/.msvc-go-wine)",
	},
	"install.default_dir": {
		EN: "No directory given, using default: %s",
		RU: "Каталог не указан, используется значение по умолчанию: %s",
	},
	"install.done": {
		EN: "Done. Add %s to PATH to use cl, link, lib, ...",
		RU: "Готово. Добавьте %s в PATH, чтобы использовать cl, link, lib и т.д.",
	},

	"env.usage": {
		EN: "usage: msvc-go-wine env (e) --bin <dest>/bin/<arch>",
		RU: "использование: msvc-go-wine env (e) --bin <dest>/bin/<arch>",
	},
	"env.unknown_arch": {
		EN: "msvc-go-wine env: unknown arch %q\n",
		RU: "msvc-go-wine env: неизвестная архитектура %q\n",
	},

	"download.host_arch": {
		EN: "Install packages for %s host architecture",
		RU: "Установка пакетов для архитектуры хоста %s",
	},
	"download.selected": {
		EN: "Selected %d packages, for a total download size of %s, install size of %s\n",
		RU: "Выбрано пакетов: %d, общий размер загрузки %s, размер после установки %s\n",
	},
	"download.default_dest": {
		EN: "--dest not set, using default: %s",
		RU: "--dest не указан, используется значение по умолчанию: %s",
	},
	"download.done": {
		EN: "Done. Next: msvc-go-wine install %s",
		RU: "Готово. Далее: msvc-go-wine install %s",
	},
	"download.wdk_skip": {
		EN: "--with-wdk: no x64/arm64 target architecture selected, skipping (no WDK package exists for x86/arm)",
		RU: "--with-wdk: не выбрана целевая архитектура x64/arm64, пропускаем (для x86/arm пакета WDK не существует)",
	},
	"download.wdk_installed": {
		EN: "Installed WDK (%s) %s at %s\n",
		RU: "WDK (%s) %s установлен в %s\n",
	},
	"download.workloads_header": {
		EN: "Available Workloads (%d):\n",
		RU: "Доступные рабочие нагрузки (Workload) (%d):\n",
	},
	"download.components_header": {
		EN: "Available Components (%d):\n",
		RU: "Доступные компоненты (Component) (%d):\n",
	},
	"download.license_prompt": {
		EN: "Do you accept the license at %s (yes/no)? ",
		RU: "Вы принимаете лицензию по адресу %s (yes/no)? ",
	},
	"download.license_reprompt": {
		EN: "Do you accept the license? Answer \"yes\" or \"no\": ",
		RU: "Вы принимаете лицензию? Ответьте «yes» или «no»: ",
	},
}
