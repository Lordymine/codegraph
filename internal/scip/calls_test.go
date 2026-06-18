package scip

import (
	"testing"

	scippb "github.com/scip-code/scip/bindings/go/scip"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestCallEdges_ControllerCallsService reproduces the M2 spike case with a
// synthetic SCIP index: a reference to the Service's getActiveCode, sitting
// inside the Controller's getActiveCode method, must yield a CALLS edge
// Controller.getActiveCode -> Service.getActiveCode (homonyms disambiguated).
func TestCallEdges_ControllerCallsService(t *testing.T) {
	ctrlFile := "apps/api/src/validation-codes/validation-codes.controller.ts"
	ctrlQN := "proj:" + ctrlFile + ".ValidationCodesController.getActiveCode"
	svcQN := "proj:apps/api/src/validation-codes/validation-codes.service.ts.ValidationCodesService.getActiveCode"

	idx := &scippb.Index{
		Documents: []*scippb.Document{{
			RelativePath: "src/validation-codes/validation-codes.controller.ts",
			Occurrences: []*scippb.Occurrence{
				// the controller's own method definition (must be ignored)
				{Symbol: symControllerGetActive, SymbolRoles: int32(scippb.SymbolRole_Definition), Range: []int32{59, 2, 17}},
				// the call site: a reference to the SERVICE method at line 64 (0-based 63)
				{Symbol: symServiceGetActive, Range: []int32{63, 8, 21}},
			},
		}},
	}

	enc := BuildEnclosing([]graph.Node{
		{Label: graph.LabelMethod, FilePath: ctrlFile, StartLine: 60, EndLine: 70, QualifiedName: ctrlQN},
	})

	edges := CallEdges(idx, "proj", "apps/api", enc)
	for _, e := range edges {
		if e.Type == graph.EdgeCalls && e.SourceQN == ctrlQN && e.TargetQN == svcQN {
			return // disambiguated CALLS edge present
		}
	}
	t.Fatalf("missing CALLS %s -> %s; got %+v", ctrlQN, svcQN, edges)
}
