package wfs

import (
	"context"
	"fmt"
	"testing"
)

func TestTransactionVersionBatchRestoresCompleteHistory(t *testing.T) {
	catalog := newPersistenceTestStore(t)
	runtime := newPersistedRuntime(t, catalog)
	var events []versionEvent
	for i := 0; i < 10001; i++ {
		events = append(events, versionEvent{action: versionActionUpdate, layerName: "roads", featureID: fmt.Sprint(i)})
	}
	events = append(events, versionEvent{action: versionActionUpdate, layerName: "roads", featureID: "0"}, versionEvent{action: versionActionDelete, layerName: "roads", featureID: "0"})
	runtime.Versions.recordEvents("ws1", events, "owner")
	records, err := catalog.ListWFSFeatureVersions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 10002 {
		t.Fatalf("recorded %d versions, want 10002", len(records))
	}
	restored := newPersistedRuntime(t, catalog)
	versions := restored.Versions.GetAllVersions("ws1", "roads", "0")
	if len(versions) != 2 || versions[0].State != VersionStateSuperseded || versions[1].State != VersionStateRetired {
		t.Fatalf("history=%+v", versions)
	}
}

func TestTransactionVersionBatchPreservesEvictionsAndTrimming(t *testing.T) {
	catalog := newPersistenceTestStore(t)
	versions := NewVersionStore(1, 1)
	versions.setPersistence(catalog, nil)
	versions.recordEvents("ws1", []versionEvent{
		{action: versionActionInsert, layerName: "roads", featureID: "0"},
		{action: versionActionUpdate, layerName: "roads", featureID: "0"},
		{action: versionActionInsert, layerName: "roads", featureID: "1"},
	}, "owner")
	records, err := catalog.ListWFSFeatureVersions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].FeatureID != "1" {
		t.Fatalf("evicted records survived: %+v", records)
	}
}
