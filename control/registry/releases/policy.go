// Copyright 2026 Mindclade. All rights reserved.
package releases

type PromotionPolicy struct {
	Required         []EvidenceKind
	RequireAllPassed bool
}

func (p PromotionPolicy) Evaluate(g EvidenceGraph) error {
	if err := g.Validate(); err != nil {
		return err
	}
	present := map[EvidenceKind]bool{}
	for _, n := range g.Nodes {
		if !p.RequireAllPassed || n.Passed {
			present[n.Kind] = true
		}
		if p.RequireAllPassed && !n.Passed {
			return invalid("release_evidence_failed", "release evidence graph contains failed evidence", nil)
		}
	}
	for _, k := range p.Required {
		if !present[k] {
			return invalid("release_evidence_missing", "release is missing mandatory evidence: "+string(k), nil)
		}
	}
	return nil
}
func ProductionPolicy() PromotionPolicy {
	return PromotionPolicy{RequireAllPassed: true, Required: []EvidenceKind{EvidenceSourceCommit, EvidenceResolvedConfig, EvidenceDataset, EvidenceTrainingRun, EvidenceCheckpoint, EvidenceModelBundle, EvidenceRuntimeBundle, EvidenceKernelQualification, EvidenceNumericalQualification, EvidenceEvaluation, EvidenceSafety, EvidenceSBOM, EvidenceProvenance, EvidenceSignature, EvidenceToolchain}}
}
