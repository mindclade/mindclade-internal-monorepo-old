package orchestration

import "testing"

func TestWorkloadEnvelopeTypeIsCanonicalBoundary(t *testing.T) {
	var w WorkloadEnvelope
	if w.Attempt != 0 {
		t.Fatal("zero value changed")
	}
}
