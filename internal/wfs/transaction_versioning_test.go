package wfs

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
)

func newVersioningTestHandler() (*workspaceHandler, *RuntimeState) {
	state := NewRuntimeState(conf.WFS{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := &workspaceHandler{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		state:  state,
	}
	return h, state
}

const insertTransactionXML = `<wfs:Transaction xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0">
  <wfs:Insert><first/></wfs:Insert>
</wfs:Transaction>`

const doubleInsertTransactionXML = `<wfs:Transaction xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0">
  <wfs:Insert><first/></wfs:Insert>
  <wfs:Insert><first/></wfs:Insert>
</wfs:Transaction>`

func TestCommittedInsertRecordsVersionMetadata(t *testing.T) {
	h, state := newVersioningTestHandler()
	defer state.Close()
	ws := transactionTestWorkspace(&atomicWriterStub{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/wfs", bytes.NewBufferString(insertTransactionXML))
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Subject: "tester", AuthMethod: identity.AuthMethodBasic}))
	w := httptest.NewRecorder()

	h.handleTransaction(w, req, ws)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	versions := state.Versions.GetAllVersions(ws.ID, "first", "1")
	if len(versions) != 1 {
		t.Fatalf("expected one recorded version, got %+v", versions)
	}
	v := versions[0]
	if v.Version != 1 || v.State != VersionStateValid {
		t.Errorf("recorded version = %+v, want valid version 1", v)
	}
	if v.ModifiedBy != "basic:tester" {
		t.Errorf("modifiedBy = %q, want basic:tester", v.ModifiedBy)
	}
}

func TestFailedTransactionRecordsNoVersionMetadata(t *testing.T) {
	h, state := newVersioningTestHandler()
	defer state.Close()
	// atomicWriterStub forces the second insert to fail, rolling back the
	// whole transaction.
	ws := transactionTestWorkspace(&atomicWriterStub{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/wfs", bytes.NewBufferString(doubleInsertTransactionXML))
	w := httptest.NewRecorder()

	h.handleTransaction(w, req, ws)
	if w.Code == http.StatusOK {
		t.Fatalf("expected transaction failure, got 200: %s", w.Body.String())
	}

	if versions := state.Versions.GetAllVersions(ws.ID, "first", "1"); len(versions) != 0 {
		t.Fatalf("failed transaction must record nothing, got %+v", versions)
	}
}
