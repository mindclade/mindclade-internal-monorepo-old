package ingestion

import "context"

type Service struct{ snapshots SnapshotRepository }

func NewService(repo SnapshotRepository) *Service { return &Service{snapshots: repo} }
func (s *Service) RegisterSnapshot(ctx context.Context, snapshot Snapshot) error {
	if s == nil || s.snapshots == nil {
		return invalid("ingestion_service_unconfigured", "ingestion service is unconfigured", nil)
	}
	return s.snapshots.Put(ctx, snapshot)
}
