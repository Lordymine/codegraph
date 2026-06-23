package memory

import "testing"

func TestNodeHeapMB_Default(t *testing.T) {
	if NodeHeapMB() < 256 {
		t.Fatalf("NodeHeapMB too small: %d", NodeHeapMB())
	}
}

func TestSystemRAMBytes_Platform(t *testing.T) {
	// On Windows/Linux with working probes, RAM should be non-zero.
	ram := systemRAMBytes()
	if ram > 0 && ram < 256*1024*1024 {
		t.Fatalf("suspicious RAM reading: %d", ram)
	}
}
