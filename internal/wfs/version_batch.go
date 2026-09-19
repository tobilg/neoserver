package wfs

import (
	"context"
	"github.com/tobilg/neoserver/internal/store"
	"time"
)

type versionBatchPersistence interface {
	ApplyWFSVersionChanges(context.Context, []store.WFSVersionChange) error
}
type versionChanges struct {
	RuntimePersistence
	changes []store.WFSVersionChange
}

func (v *versionChanges) PutWFSFeatureVersion(_ context.Context, record store.WFSFeatureVersionRecord) error {
	v.changes = append(v.changes, store.WFSVersionChange{Record: record})
	return nil
}
func (v *versionChanges) DeleteWFSFeatureVersions(_ context.Context, workspace, layer, feature string) error {
	v.changes = append(v.changes, store.WFSVersionChange{Record: store.WFSFeatureVersionRecord{WorkspaceID: workspace, LayerID: layer, FeatureID: feature}, DeleteAll: true})
	return nil
}
func (v *versionChanges) DeleteWFSFeatureVersionsBelow(_ context.Context, workspace, layer, feature string, min int) error {
	v.changes = append(v.changes, store.WFSVersionChange{Record: store.WFSFeatureVersionRecord{WorkspaceID: workspace, LayerID: layer, FeatureID: feature}, MinVersion: min})
	return nil
}

func (vs *VersionStore) recordEvents(workspace string, events []versionEvent, owner string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	original := vs.persist
	batch, canBatch := original.(versionBatchPersistence)
	changes := &versionChanges{RuntimePersistence: original}
	if canBatch {
		vs.persist = changes
	}
	defer func() { vs.persist = original }()
	for _, event := range events {
		switch event.action {
		case versionActionInsert:
			vs.recordInsertLocked(workspace, event.layerName, event.featureID, owner)
		case versionActionUpdate:
			vs.recordUpdateLocked(workspace, event.layerName, event.featureID, owner)
		case versionActionDelete:
			vs.recordDeleteLocked(workspace, event.layerName, event.featureID)
		}
	}
	if canBatch && len(changes.changes) > 0 {
		// Metadata remains best-effort, but cannot hold a committed HTTP write
		// indefinitely. One catalog transaction replaces thousands of fsyncs.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := batch.ApplyWFSVersionChanges(ctx, changes.changes); err != nil && vs.logger != nil {
			vs.logger.Warn("persist WFS transaction version batch", "workspace", workspace, "error", err)
		}
	}
}
