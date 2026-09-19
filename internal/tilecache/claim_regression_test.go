package tilecache

import (
	"context"
	"sync"
	"testing"
)

func TestClaimFindsOldQueuedWorkBeyondHistoryLimit(t *testing.T) {
	m := newTestManager(t, t.TempDir(), 1<<20)
	defer m.Close()
	ctx := context.Background()
	old, err := m.JobStore().CreateTileCacheJob(ctx, CreateJobInput{WorkspaceID: "workspace", Request: JobRequest{Operation: OperationSeed}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.index.db.Exec(`INSERT INTO tile_cache_jobs(id,workspace_id,request_json,status,created_at)
 SELECT 'history-'||i,'workspace','{}','succeeded',current_timestamp+INTERVAL '1 hour' FROM range(1005) t(i)`); err != nil {
		t.Fatal(err)
	}
	history, err := m.JobStore().ListTileCacheJobs(ctx, "", 1000)
	if err != nil || len(history) != 1000 {
		t.Fatalf("history=%d err=%v", len(history), err)
	}
	for _, job := range history {
		if job.ID == old.ID {
			t.Fatal("fixture does not exercise pagination boundary")
		}
	}
	var wg sync.WaitGroup
	claims := make(chan *Job, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := m.JobStore().ClaimNextTileCacheJob(ctx)
			claims <- job
			failures <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(failures)
	count := 0
	for job := range claims {
		if job != nil {
			count++
			if job.ID != old.ID || job.Status != JobRunning {
				t.Fatalf("wrong claim: %+v", job)
			}
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("claimed %d times", count)
	}
}
