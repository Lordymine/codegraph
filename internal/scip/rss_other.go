//go:build !linux

package scip

// peakChildRSS is not implemented on this platform; returns 0.
func peakChildRSS(pid int, until <-chan struct{}) uint64 {
	<-until
	return 0
}
