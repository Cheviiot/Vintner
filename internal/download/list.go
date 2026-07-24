package download

import "sort"

// PackagesByType returns the best (arch/language-matching) variant of every
// package in idx whose Type equals kind (e.g. "Workload" or "Component"),
// sorted by ID.
func PackagesByType(idx Index, kind string) []*Package {
	var ret []*Package
	for _, variants := range idx {
		p := variants[0]
		if p.Type == kind {
			ret = append(ret, p)
		}
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].ID < ret[j].ID })
	return ret
}
