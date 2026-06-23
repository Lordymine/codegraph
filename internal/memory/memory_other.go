//go:build !linux && !windows

package memory

func systemRAMBytes() uint64 { return 0 }
