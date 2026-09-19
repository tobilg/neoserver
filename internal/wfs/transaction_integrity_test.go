package wfs

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
)

type integrityWriter struct {
	datasource.DataSource
	operations  []string
	values      map[string]interface{}
	ids         []string
	mutationErr error
	committed   bool
}

func (*integrityWriter) GetLayerInfo(context.Context, string) (*datasource.LayerInfo, error) {
	return &datasource.LayerInfo{Name: "first", GeometryColumn: "geom", GeometryType: "POINT", SRID: 4326, IDColumn: "id", Properties: []datasource.PropertyInfo{{Name: "id", JSONType: datasource.JSONTypeInteger}, {Name: "name", JSONType: datasource.JSONTypeString}, {Name: "count", JSONType: datasource.JSONTypeInteger}, {Name: "active", JSONType: datasource.JSONTypeBoolean}}}, nil
}
func (s *integrityWriter) AtomicWrite(_ context.Context, f func(datasource.FeatureWriter) error) error {
	err := f(s)
	s.committed = err == nil
	return err
}
func (s *integrityWriter) Insert(context.Context, string, []datasource.FeatureData) ([]string, error) {
	s.operations = append(s.operations, "insert")
	return []string{"new"}, s.mutationErr
}
func (s *integrityWriter) Update(ctx context.Context, layer string, props map[string]interface{}, filter string, args []interface{}) (int, error) {
	ids, err := s.UpdateReturning(ctx, layer, props, filter, args)
	return len(ids), err
}
func (s *integrityWriter) Delete(ctx context.Context, layer, filter string, args []interface{}) (int, error) {
	ids, err := s.DeleteReturning(ctx, layer, filter, args)
	return len(ids), err
}
func (s *integrityWriter) Replace(context.Context, string, datasource.FeatureData, string, []interface{}) ([]string, error) {
	s.operations = append(s.operations, "replace")
	return s.ids, s.mutationErr
}
func (s *integrityWriter) UpdateReturning(_ context.Context, _ string, props map[string]interface{}, _ string, _ []interface{}) ([]string, error) {
	s.operations = append(s.operations, "update")
	s.values = props
	return s.ids, s.mutationErr
}
func (s *integrityWriter) DeleteReturning(context.Context, string, string, []interface{}) ([]string, error) {
	s.operations = append(s.operations, "delete")
	return s.ids, s.mutationErr
}

func integrityHandler() *workspaceHandler {
	return &workspaceHandler{cfg: conf.Config{}, state: &RuntimeState{Locks: NewLockStore(300, 20, 20, 20000)}}
}
func integrityTransaction(t *testing.T, body string) *WFSTransaction {
	t.Helper()
	tx, err := ParseTransactionRequest([]byte(`<wfs:Transaction service="WFS" version="2.0.0" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + body + `</wfs:Transaction>`))
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

const integrityFilter = `<fes:Filter><fes:ResourceId rid="first.1"/></fes:Filter>`

func TestTransactionsCheckEveryAffectedLockBeforeCommit(t *testing.T) {
	for _, op := range []string{
		`<wfs:Update typeName="first"><wfs:Property><wfs:ValueReference>name</wfs:ValueReference><wfs:Value>new</wfs:Value></wfs:Property></wfs:Update>`,
		`<wfs:Delete typeName="first">` + integrityFilter + `</wfs:Delete>`,
		`<wfs:Replace><first><name>new</name></first>` + integrityFilter + `</wfs:Replace>`,
	} {
		for _, failQuery := range []bool{false, true} {
			s := &integrityWriter{}
			for i := 1; i <= 10001; i++ {
				s.ids = append(s.ids, fmt.Sprint(i))
			}
			if failQuery {
				s.mutationErr = errors.New("affected-row query failed")
			}
			h := integrityHandler()
			lock, _, err := h.state.Locks.AcquireLockOwned("ws-1", map[string][]string{"first": {"10001"}}, 60, LockActionAll, "owner")
			if err != nil {
				t.Fatal(err)
			}
			tx := integrityTransaction(t, op)
			if _, err := h.executeTransaction(context.Background(), transactionTestWorkspace(s, nil), tx); err == nil || s.committed {
				t.Fatalf("rejected mutation committed: %v", err)
			}
			if !failQuery {
				tx.LockId = lock.LockID
				if _, err := h.executeTransaction(context.Background(), transactionTestWorkspace(s, nil), tx); err != nil || !s.committed {
					t.Fatalf("valid lock rejected: %v", err)
				}
			}
		}
	}
}

func TestTransactionDocumentOrderAndHandles(t *testing.T) {
	s := &integrityWriter{ids: []string{"1"}}
	tx := integrityTransaction(t, `<wfs:Delete typeName="first">`+integrityFilter+`</wfs:Delete><wfs:Insert handle="created"><first><name>new</name></first></wfs:Insert><wfs:Replace handle="replaced"><first><name>replaced</name></first>`+integrityFilter+`</wfs:Replace><wfs:Update typeName="first" handle="edited"><wfs:Property><wfs:ValueReference>name</wfs:ValueReference><wfs:Value>edited</wfs:Value></wfs:Property></wfs:Update>`)
	result, err := integrityHandler().executeTransaction(context.Background(), transactionTestWorkspace(s, nil), tx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.operations, []string{"delete", "insert", "replace", "update"}) {
		t.Fatal(s.operations)
	}
	if result.InsertResults.Features[0].Handle != "created" || result.UpdateResults.Features[0].Handle != "edited" || result.ReplaceResults.Features[0].Handle != "replaced" {
		t.Fatal("action handles lost")
	}
}

func TestTransactionValuesPreserveXMLAndTypes(t *testing.T) {
	precise := "12345678901234567.12345678901234567890"
	info := &datasource.LayerInfo{Properties: []datasource.PropertyInfo{{Name: "precise", Type: "numeric(38,20)", JSONType: datasource.JSONTypeNumber}}}
	if value, err := typedScalar(info, "precise", precise); err != nil || value != precise {
		t.Fatalf("exact decimal changed: %v %v", value, err)
	}
	if _, err := typedScalar(info, "precise", "1/3"); err == nil {
		t.Fatal("non-decimal numeric input accepted")
	}
	for _, tc := range []struct {
		raw  string
		want interface{}
	}{
		{`<wfs:Value> A &amp; B &lt;世界&gt; </wfs:Value>`, " A & B <世界> "},
		{`<wfs:Value/>`, ""},
		{`<wfs:Value xsi:nil="true"/>`, nil},
	} {
		s := &integrityWriter{ids: []string{"1"}}
		tx := integrityTransaction(t, `<wfs:Update typeName="first"><wfs:Property><wfs:ValueReference>name</wfs:ValueReference>`+tc.raw+`</wfs:Property></wfs:Update>`)
		if _, err := integrityHandler().executeTransaction(context.Background(), transactionTestWorkspace(s, nil), tx); err != nil {
			t.Fatal(err)
		}
		if s.values["name"] != tc.want {
			t.Fatalf("got %#v want %#v", s.values["name"], tc.want)
		}
	}
	s := &integrityWriter{}
	tx := integrityTransaction(t, `<wfs:Update typeName="first"><wfs:Property><wfs:ValueReference>count</wfs:ValueReference><wfs:Value>12</wfs:Value></wfs:Property><wfs:Property><wfs:ValueReference>active</wfs:ValueReference><wfs:Value>true</wfs:Value></wfs:Property><wfs:Property><wfs:ValueReference>geom</wfs:ValueReference><wfs:Value><gml:Point srsName="urn:ogc:def:crs:OGC::CRS84"><gml:pos>10 20</gml:pos></gml:Point></wfs:Value></wfs:Property></wfs:Update>`)
	if _, err := integrityHandler().executeTransaction(context.Background(), transactionTestWorkspace(s, nil), tx); err != nil {
		t.Fatal(err)
	}
	if s.values["count"] != int64(12) || s.values["active"] != true {
		t.Fatal(s.values)
	}
	geom, ok := s.values["geom"].(datasource.GeometryValue)
	if !ok || geom.SRID != 4326 || !strings.Contains(geom.GML, "EPSG:4326") {
		t.Fatal(geom)
	}
}

func TestTransactionGeometryCRSPrecedenceAndValidation(t *testing.T) {
	h := integrityHandler()
	for _, tc := range []struct {
		raw, inherited, expected string
		srid                     int
	}{
		{`<gml:Point><gml:pos>10 20</gml:pos></gml:Point>`, "EPSG:3857", "EPSG:3857", 3857},
		{`<gml:Point srsName="CRS:84"><gml:pos>10 20</gml:pos></gml:Point>`, "EPSG:3857", "EPSG:4326", 4326},
		{`<gml:Point srsName="http://www.opengis.net/def/crs/EPSG/0/4326"><gml:pos>20 10</gml:pos></gml:Point>`, "", "urn:ogc:def:crs:EPSG::4326", 4326},
	} {
		got, err := h.transactionGeometry(tc.raw, tc.inherited)
		if err != nil {
			t.Fatal(err)
		}
		if got.SRID != tc.srid || !strings.Contains(got.GML, tc.expected) {
			t.Fatal(got)
		}
		var node struct{ XMLName xml.Name }
		if err := xml.Unmarshal([]byte(got.GML), &node); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`<gml:Point srsName="EPSG:99999"/>`, `<gml:Point/><gml:Point/>`, `<name>text</name>`, `<gml:Point href="https://example.com/a"/>`} {
		if _, err := h.transactionGeometry(raw, ""); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLockGrantWaitsForWriteAndUnrelatedWorkspacesProceed(t *testing.T) {
	h := integrityHandler()
	release, err := h.state.Locks.writes.acquire(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := h.state.Locks.AcquireLockOwnedContext(ctx, "ws-1", map[string][]string{"first": {"1"}}, 60, LockActionAll, "owner"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock granted during write: %v", err)
	}
	if _, _, err := h.state.Locks.AcquireLockOwnedContext(context.Background(), "other", map[string][]string{"first": {"1"}}, 60, LockActionAll, "owner"); err != nil {
		t.Fatal(err)
	}
	release()
	if _, _, err := h.state.Locks.AcquireLockOwnedContext(context.Background(), "ws-1", map[string][]string{"first": {"1"}}, 60, LockActionAll, "owner"); err != nil {
		t.Fatal(err)
	}
	h.state.Locks.writes.mu.Lock()
	defer h.state.Locks.writes.mu.Unlock()
	if len(h.state.Locks.writes.entries) != 0 {
		t.Fatal("idle write gates leaked")
	}
}
