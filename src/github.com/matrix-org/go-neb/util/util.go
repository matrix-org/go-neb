package util

import (
	"sort"
)

// Difference returns the elements that are only in the first list and
// the elements that are only in the second. As a side-effect this sorts
// the input lists in-place.
func Difference(a, b []string) (onlyA, onlyB []string) {
	sort.Strings(a)
	sort.Strings(b)
	for {
		if len(b) == 0 {
			onlyA = append(onlyA, a...)
			return
		}
		if len(a) == 0 {
			onlyB = append(onlyB, b...)
			return
		}
		xA := a[0]
		xB := b[0]
		if xA < xB {
			onlyA = append(onlyA, xA)
			a = a[1:]
		} else if xA > xB {
			onlyB = append(onlyB, xB)
			b = b[1:]
		} else {
			a = a[1:]
			b = b[1:]
		}
	}
}
