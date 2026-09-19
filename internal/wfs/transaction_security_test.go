package wfs

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

type atomicWriterStub struct {
	datasource.DataSource
	calls     int
	committed int
	pending   int
}

func (s *atomicWriterStub) AtomicWrite(ctx context.Context, fn func(datasource.FeatureWriter) error) error {
	s.pending = 0
	err := fn(s)
	if err == nil {
		s.committed += s.pending
	}
	s.pending = 0
	return err
}

func (s *atomicWriterStub) Insert(context.Context, string, []datasource.FeatureData) ([]string, error) {
	s.calls++
	if s.calls == 2 {
		return nil, errors.New("forced failure")
	}
	s.pending++
	return []string{"1"}, nil
}

func (*atomicWriterStub) Update(context.Context, string, map[string]interface{}, string, []interface{}) (int, error) {
	return 0, nil
}

func (*atomicWriterStub) Delete(context.Context, string, string, []interface{}) (int, error) {
	return 0, nil
}

func (*atomicWriterStub) Replace(context.Context, string, datasource.FeatureData, string, []interface{}) ([]string, error) {
	return nil, nil
}

func TestTransactionRejectsCrossServiceWrites(t *testing.T) {
	ws := transactionTestWorkspace(&atomicWriterStub{}, &atomicWriterStub{})
	tx := &WFSTransaction{Updates: []WFSUpdate{{TypeName: "first"}, {TypeName: "second"}}}

	_, err := (&workspaceHandler{}).executeTransaction(context.Background(), ws, tx)
	var reqErr *RequestError
	if !errors.As(err, &reqErr) || reqErr.Code != ExceptionOperationNotSupported {
		t.Fatalf("expected cross-service OperationNotSupported, got %v", err)
	}
}

func TestTransactionRollsBackAllWritesOnFailure(t *testing.T) {
	writer := &atomicWriterStub{}
	ws := transactionTestWorkspace(writer, nil)
	tx := &WFSTransaction{Inserts: []WFSInsert{
		{Features: []XMLFeature{{XMLName: xmlName("first")}}},
		{Features: []XMLFeature{{XMLName: xmlName("first")}}},
	}}

	if _, err := (&workspaceHandler{}).executeTransaction(context.Background(), ws, tx); err == nil {
		t.Fatal("expected second insert to fail")
	}
	if writer.committed != 0 {
		t.Fatalf("committed writes = %d, want 0", writer.committed)
	}
}

func TestTransactionUnknownInsertUsesInvalidValue(t *testing.T) {
	tx := &WFSTransaction{Inserts: []WFSInsert{{Features: []XMLFeature{{XMLName: xmlName("unknown")}}}}}

	_, err := (&workspaceHandler{}).executeTransaction(context.Background(), transactionTestWorkspace(&atomicWriterStub{}, nil), tx)
	var reqErr *RequestError
	if !errors.As(err, &reqErr) || reqErr.Code != ExceptionInvalidValue {
		t.Fatalf("expected InvalidValue, got %v", err)
	}
}

func TestTransactionReleaseAction(t *testing.T) {
	tests := []struct {
		name          string
		releaseAction string
		wantReleased  bool
	}{
		{name: "explicit ALL", releaseAction: ` releaseAction="ALL"`, wantReleased: true},
		{name: "default ALL", wantReleased: true},
		{name: "SOME", releaseAction: ` releaseAction="SOME"`, wantReleased: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locks := NewLockStore(300, 10, 10, 100)
			lock, _, err := locks.AcquireLockOwned("ws-1", map[string][]string{"roads": {"1"}}, 60, LockActionAll, "basic:tester")
			if err != nil {
				t.Fatal(err)
			}
			h := &workspaceHandler{state: &RuntimeState{Locks: locks}}
			body := `<wfs:Transaction xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0" lockId="` + lock.LockID + `"` + tt.releaseAction + `/>`
			req := httptest.NewRequest(http.MethodPost, "/wfs", bytes.NewBufferString(body))
			req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Subject: "tester", AuthMethod: identity.AuthMethodBasic}))
			w := httptest.NewRecorder()

			h.handleTransaction(w, req, &workspace.Workspace{ID: "ws-1", Services: map[string]*workspace.Service{}})

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if released := locks.GetLock(lock.LockID) == nil; released != tt.wantReleased {
				t.Fatalf("released = %t, want %t", released, tt.wantReleased)
			}
		})
	}
}

func transactionTestWorkspace(first datasource.DataSource, second datasource.DataSource) *workspace.Workspace {
	services := map[string]*workspace.Service{
		"svc-1": {ID: "svc-1", Enabled: true, DataSource: first, Layers: map[string]*workspace.Layer{
			"first": {ID: "layer-1", PublicID: "first", SourceLayer: "first", Enabled: true},
		}},
	}
	if second != nil {
		services["svc-2"] = &workspace.Service{ID: "svc-2", Enabled: true, DataSource: second, Layers: map[string]*workspace.Layer{
			"second": {ID: "layer-2", PublicID: "second", SourceLayer: "second", Enabled: true},
		}}
	}
	return &workspace.Workspace{ID: "ws-1", Services: services}
}

func xmlName(local string) xml.Name {
	return xml.Name{Local: local}
}
