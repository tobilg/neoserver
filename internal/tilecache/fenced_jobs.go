package tilecache

import "context"

// fencedJobStore keeps the durable job tables under the same ownership gate as
// cache payloads and metadata. Reads remain available after lease loss.
type fencedJobStore struct {
	manager *Manager
	store   JobStore
}

func (s *fencedJobStore) mutate(fn func() error) error {
	end, err := s.manager.beginMutation()
	if err != nil {
		return err
	}
	defer end()
	return fn()
}

func (s *fencedJobStore) CreateTileCacheJob(ctx context.Context, input CreateJobInput) (job *Job, err error) {
	err = s.mutate(func() error { job, err = s.store.CreateTileCacheJob(ctx, input); return err })
	return
}

func (s *fencedJobStore) ClaimNextTileCacheJob(ctx context.Context) (job *Job, err error) {
	err = s.mutate(func() error { job, err = s.store.ClaimNextTileCacheJob(ctx); return err })
	return
}

func (s *fencedJobStore) GetTileCacheJob(ctx context.Context, id string) (*Job, error) {
	return s.store.GetTileCacheJob(ctx, id)
}

func (s *fencedJobStore) ListTileCacheJobs(ctx context.Context, workspaceID string, limit int) ([]*Job, error) {
	return s.store.ListTileCacheJobs(ctx, workspaceID, limit)
}

func (s *fencedJobStore) UpdateTileCacheJob(ctx context.Context, id string, update JobUpdate) (job *Job, err error) {
	err = s.mutate(func() error { job, err = s.store.UpdateTileCacheJob(ctx, id, update); return err })
	return
}

func (s *fencedJobStore) RequestTileCacheJobCancel(ctx context.Context, id string) error {
	return s.mutate(func() error { return s.store.RequestTileCacheJobCancel(ctx, id) })
}

func (s *fencedJobStore) ResetInterruptedTileCacheJobs(ctx context.Context) error {
	return s.mutate(func() error { return s.store.ResetInterruptedTileCacheJobs(ctx) })
}

func (s *fencedJobStore) ReplaceTileCacheJobChunks(ctx context.Context, jobID string, chunks []JobChunk) error {
	return s.mutate(func() error { return s.store.ReplaceTileCacheJobChunks(ctx, jobID, chunks) })
}

func (s *fencedJobStore) ListTileCacheJobChunks(ctx context.Context, jobID string) ([]JobChunk, error) {
	return s.store.ListTileCacheJobChunks(ctx, jobID)
}

func (s *fencedJobStore) UpdateTileCacheJobChunk(ctx context.Context, chunk JobChunk) error {
	return s.mutate(func() error { return s.store.UpdateTileCacheJobChunk(ctx, chunk) })
}
