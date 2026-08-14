package ingestion

import "mindclade.internal/control/orchestration"

func ValidIngestionStage(kind orchestration.StageKind) bool {
	switch kind {
	case orchestration.StageIngestion, orchestration.StageCurate, orchestration.StagePreprocess, orchestration.StageReferenceBuild:
		return true
	}
	return false
}
