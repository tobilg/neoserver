package mosaiccatalog

import (
	"context"
	"errors"
	"testing"
)

func TestCancellationSurvivesStaleWorkerAndRestart(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()
	job, err := c.createJob(ctx, "ws", "svc", "test", HarvestRequest{Mode: HarvestAppend})
	if err != nil {
		t.Fatal(err)
	}
	job.Status = JobRunning
	if err := c.updateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := c.requestCancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job.Processed = 1
	if err := c.updateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	current, err := c.getJob(ctx, job.ID)
	if err != nil || !current.CancelRequested || current.Status != JobCancelling {
		t.Fatalf("cancellation overwritten: %+v %v", current, err)
	}
	if _, err := c.activateJobGeneration(ctx, "ws", "svc", 1, job.ID); !errors.Is(err, errHarvestCancelled) {
		t.Fatalf("cancelled job activated: %v", err)
	}
	if err := c.resetInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.updateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	current, err = c.getJob(ctx, job.ID)
	if err != nil || current.Status != JobCancelled || !current.CancelRequested {
		t.Fatalf("terminal job resurrected: %+v %v", current, err)
	}
}

func TestActivationIsCancellationCommitBoundary(t *testing.T) {
	c := newTestCatalog(t)
	ctx := context.Background()
	job, err := c.createJob(ctx, "ws1", "svc", "test", HarvestRequest{Mode: HarvestAppend})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := c.beginGeneration(ctx, "ws1", "svc", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.upsertGranule(ctx, testGranule("svc", gen, "/data/a.tif")); err != nil {
		t.Fatal(err)
	}
	job.Status, job.Generation = JobRunning, gen
	if err = c.updateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err = c.activateJobGeneration(ctx, "ws1", "svc", gen, job.ID); err != nil {
		t.Fatal(err)
	}
	if err = c.requestCancel(ctx, job.ID); err == nil {
		t.Fatal("cancellation accepted after activation committed")
	}
	current, err := c.getJob(ctx, job.ID)
	if err != nil || current.CancelRequested {
		t.Fatal("late cancellation changed durable intent")
	}
}
