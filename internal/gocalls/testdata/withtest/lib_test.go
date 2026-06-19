package withtest

import "testing"

func TestTarget(t *testing.T) {
	if Target() != 42 {
		t.Fail()
	}
}
