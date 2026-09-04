package onepce

import (
	"fmt"
	"sort"
	"strconv"
)

func fmtSscanf(name string, f *uint64) (int, error) { return fmt.Sscanf(name, "screen-%d.bin", f) }

func sortUint64(s []uint64) { sort.Slice(s, func(i, j int) bool { return s[i] < s[j] }) }

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

func fmtSscanfWindow(text string, start, stop *uint64) (int, error) {
	return fmt.Sscanf(text, "%d-%d", start, stop)
}
