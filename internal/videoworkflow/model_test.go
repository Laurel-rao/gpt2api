package videoworkflow

import (
	"strings"
	"testing"
)

func TestGraphSQLRoundTrip(t *testing.T) {
	want := testGraph()
	value, err := want.Value()
	if err != nil {
		t.Fatal(err)
	}
	var fromBytes Graph
	if err := fromBytes.Scan(value); err != nil {
		t.Fatal(err)
	}
	var fromString Graph
	if err := fromString.Scan(string(value.([]byte))); err != nil {
		t.Fatal(err)
	}
	if len(fromBytes.Nodes) != len(want.Nodes) || len(fromString.Edges) != len(want.Edges) {
		t.Fatalf("round trip changed graph: bytes=%+v string=%+v", fromBytes, fromString)
	}
	if err := fromBytes.Scan(42); err == nil {
		t.Fatal("unsupported scan type was accepted")
	}
	if err := fromBytes.Scan(nil); err != nil || len(fromBytes.Nodes) != 0 {
		t.Fatalf("nil scan graph=%+v err=%v", fromBytes, err)
	}
	if _, err := want.Settings.Value(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedIDsUseDomainPrefixes(t *testing.T) {
	values := map[string]string{
		"vwf_": NewWorkflowID(), "vwr_": NewRunID(), "vwn_": NewNodeRunID(),
		"vwa_": NewAssetID(), "vwv_": NewVersionID(), "vwc_": NewChargeID(),
	}
	for prefix, value := range values {
		if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) {
			t.Fatalf("id %q does not use prefix %q", value, prefix)
		}
	}
}
