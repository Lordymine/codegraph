//go:build linux

package scip

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// peakChildRSS polls /proc/<pid>/status until until is closed and returns the
// maximum VmRSS observed (bytes). Used to surface scip-typescript memory use on
// Linux and WSL without any user configuration.
func peakChildRSS(pid int, until <-chan struct{}) uint64 {
	var peak uint64
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-until:
			return peak
		case <-tick.C:
			if rss := procRSS(pid); rss > peak {
				peak = rss
			}
		}
	}
}

func procRSS(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
