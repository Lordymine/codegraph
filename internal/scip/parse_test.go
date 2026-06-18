package scip

import (
	"fmt"
	"testing"

	scippb "github.com/scip-code/scip/bindings/go/scip"
)

// Real symbols observed in the M2 spike on apps/api.
const (
	symServiceGetActive    = "scip-typescript npm api 0.0.1 src/validation-codes/`validation-codes.service.ts`/ValidationCodesService#getActiveCode()."
	symControllerGetActive = "scip-typescript npm api 0.0.1 src/validation-codes/`validation-codes.controller.ts`/ValidationCodesController#getActiveCode()."
)

func TestSymbolToQN_ServiceMethod(t *testing.T) {
	// Debug: surface the descriptor shape so a mismatch is diagnosable.
	if p, err := scippb.ParseSymbol(symServiceGetActive); err == nil {
		for _, d := range p.Descriptors {
			fmt.Printf("desc name=%q suffix=%v\n", d.Name, d.Suffix)
		}
	} else {
		t.Logf("parse err: %v", err)
	}

	qn, ok := symbolToQN(symServiceGetActive, "proj", "apps/api")
	if !ok {
		t.Fatal("service symbol not mapped")
	}
	want := "proj:apps/api/src/validation-codes/validation-codes.service.ts.ValidationCodesService.getActiveCode"
	if qn != want {
		t.Errorf("qn  = %q\nwant = %q", qn, want)
	}
}

func TestSymbolToQN_DistinctFromController(t *testing.T) {
	svc, _ := symbolToQN(symServiceGetActive, "proj", "apps/api")
	ctrl, _ := symbolToQN(symControllerGetActive, "proj", "apps/api")
	if svc == ctrl {
		t.Fatalf("service and controller getActiveCode mapped to same QN %q", svc)
	}
}
