package download

import (
	"regexp"
	"sort"
	"strings"
)

// Index maps a lowercased package id to every variant sharing that id,
// sorted by arch/language priority (best match first).
type Index map[string][]*Package

// BuildIndex indexes every package in m, prioritizing variants matching
// hostArch and language ("" language means no language preference beyond
// the default "en").
func BuildIndex(m *Manifest, hostArch, language string) Index {
	if language == "" {
		language = "en"
	}
	idx := Index{}
	for i := range m.Packages {
		p := &m.Packages[i]
		key := strings.ToLower(p.ID)
		idx[key] = append(idx[key], p)
	}
	for key, list := range idx {
		list := list
		sort.SliceStable(list, func(i, j int) bool {
			return comparePackages(hostArch, language, list[i], list[j]) < 0
		})
		idx[key] = list
	}
	return idx
}

// comparePackages ranks arch match first, then language match.
func comparePackages(arch, language string, a, b *Package) int {
	archOrd := func(field string, x *Package) int {
		if arch == "" {
			return 0
		}
		var v string
		switch field {
		case "chip":
			v = x.Chip
		case "machineArch":
			v = x.MachineArch
		case "productArch":
			v = x.ProductArch
		}
		v = strings.ToLower(v)
		if v == "" || v == "neutral" {
			return 0
		}
		if v == arch {
			return -1
		}
		return 1
	}
	for _, field := range []string{"chip", "machineArch", "productArch"} {
		if r := archOrd(field, a) - archOrd(field, b); r != 0 {
			return r
		}
	}

	lang := strings.ToLower(language)
	countryLang := strings.Contains(lang, "-")
	langPriority := func(x *Package) int {
		xl := strings.ToLower(x.Language)
		if xl == "" {
			return 0
		}
		if (countryLang && xl == lang) || (!countryLang && strings.HasPrefix(xl, lang+"-")) {
			return 2
		}
		if strings.HasPrefix(xl, "en-") {
			return 1
		}
		return 0
	}
	ap, bp := langPriority(a), langPriority(b)
	if ap > bp {
		return -1
	}
	if ap < bp {
		return 1
	}
	return 0
}

// Find looks up id (case-insensitive), preferring a candidate matching
// constraints' chip/machineArch, falling back to the highest-priority
// variant. Returns nil if id isn't in the index at all.
func (idx Index) Find(id string, constraints map[string]string) *Package {
	candidates, ok := idx[strings.ToLower(id)]
	if !ok || len(candidates) == 0 {
		return nil
	}
	for _, p := range candidates {
		matched := true
		for _, k := range []string{"chip", "machineArch"} {
			want, has := constraints[k]
			if !has {
				continue
			}
			var got string
			if k == "chip" {
				got = p.Chip
			} else {
				got = p.MachineArch
			}
			if !strings.EqualFold(got, want) {
				matched = false
				break
			}
		}
		if matched {
			return p
		}
	}
	return candidates[0]
}

var knownHostArchs = []string{"x86", "x64", "arm64"}

// HostArchCompatible reports whether p can run on host: some packages
// encode their host arch in the id itself (e.g.
// Microsoft.VisualCpp.Tools.HostARM64.*).
func HostArchCompatible(p *Package, host string) bool {
	if host == "" {
		return true
	}
	id := strings.ToLower(p.ID)
	for _, a := range knownHostArchs {
		if strings.Contains(id, "host"+a) {
			return a == host
		}
	}
	for _, v := range []string{p.Chip, p.MachineArch, p.ProductArch} {
		v = strings.ToLower(v)
		if v == "" || v == "neutral" {
			continue
		}
		if v != host {
			return false
		}
	}
	return true
}

var reTargetArch = regexp.MustCompile(`\.target(x86|x64|arm64|arm)(\W|$)`)

// TargetArchCompatible reports whether p is wanted for archs: packages
// naming a target arch in their id (e.g. ...HostX64.TargetX64) are only
// wanted when that arch was requested.
func TargetArchCompatible(p *Package, archs []string) bool {
	if archs == nil {
		return true
	}
	m := reTargetArch.FindStringSubmatch(strings.ToLower(p.ID))
	if m == nil {
		return true
	}
	for _, a := range archs {
		if a == m[1] {
			return true
		}
	}
	return false
}
