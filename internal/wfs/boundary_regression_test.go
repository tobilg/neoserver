package wfs

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/workspace"
)

func TestFilterNamespaceAndFailureBoundaries(t *testing.T) {
	for _, prefix := range []string{"fes:", "f:", ""} {
		raw := `<Transaction xmlns="` + NSWfs + `" xmlns:fes="` + NSFes + `" xmlns:f="` + NSFes + `"><Update typeName="first"><Property><ValueReference>name</ValueReference><Value>changed</Value></Property><` + prefix + `Filter xmlns="` + NSFes + `"><` + prefix + `ResourceId rid="first.2"/></` + prefix + `Filter></Update></Transaction>`
		tx, err := ParseTransactionRequest([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		filter, err := extractFilterFromXML(tx.Updates[0].FilterRaw)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseFESFilter(filter)
		if err != nil {
			t.Fatal(err)
		}
		sql, args, _, err := CompileFES(parsed, FESCompileOptions{CollectionID: "first", StartParamIndex: 1})
		if err != nil || sql == "" || len(args) != 1 || args[0] != "2" {
			t.Fatalf("lost %s filter: %q %v %v", prefix, sql, args, err)
		}
	}
	for _, filter := range []string{`<Filter/>`, `<Filter><Unknown/></Filter>`, `<Filter xmlns="urn:wrong"><ResourceId rid="first.2"/></Filter>`, `<Filter><And/></Filter>`, integrityFilter + integrityFilter, `<fes:Filter>`} {
		if _, err := extractFilterFromXML(filter); err == nil {
			t.Fatalf("invalid filter accepted: %s", filter)
		}
	}
	for _, raw := range []string{`<Filter><Or><ResourceId rid="first.1"/><x:Unknown xmlns:x="urn:wrong"/></Or></Filter>`, `<Filter><ResourceId rid="first.1"/><PropertyIsNull><ValueReference>name</ValueReference></PropertyIsNull></Filter>`} {
		if _, err := ParseFESFilter(raw); err == nil {
			t.Fatalf("ambiguous filter accepted: %s", raw)
		}
	}
	var query XMLQuery
	if err := xml.Unmarshal([]byte(`<Query xmlns:f="`+NSFes+`"><f:Filter><f:ResourceId rid="first.2"/></f:Filter></Query>`), &query); err != nil {
		t.Fatal(err)
	}
	if filter, err := extractFilterFromXML(query.FilterRaw); err != nil || filter == "" {
		t.Fatalf("query filter lost: %v", err)
	}
}

func TestTransactionsRejectSQLViewsBeforeWriting(t *testing.T) {
	for _, body := range []string{`<wfs:Insert><first><name>x</name></first></wfs:Insert>`, `<wfs:Update typeName="first"/>`, `<wfs:Delete typeName="first">` + integrityFilter + `</wfs:Delete>`, `<wfs:Replace><first><name>x</name></first>` + integrityFilter + `</wfs:Replace>`} {
		s := &integrityWriter{}
		ws := transactionTestWorkspace(s, nil)
		for _, service := range ws.Services {
			for _, layer := range service.Layers {
				layer.IsSQLView = true
			}
		}
		_, err := integrityHandler().executeTransaction(context.Background(), ws, integrityTransaction(t, body))
		if err == nil || !strings.Contains(err.Error(), "read-only") || len(s.operations) != 0 || s.committed {
			t.Fatalf("SQL view reached writer: %v", err)
		}
	}
}

func TestStableSourceLocksSurviveAliasesRenameAndRestore(t *testing.T) {
	svc := &workspace.Service{ID: "source-1"}
	layer := &workspace.Layer{SourceLayer: "public.roads"}
	key := sourceLockKey(svc, layer, "roads")
	alias := sourceLockKey(svc, layer, "roadscopy")
	locks := NewLockStore(60, 10, 10, 100)
	lock, _, err := locks.AcquireLock("ws", map[string][]string{key: {"1"}}, 60, LockActionAll)
	if err != nil {
		t.Fatal(err)
	}
	if err := locks.CheckFeatureLock("ws", alias, "1", ""); err == nil {
		t.Fatal("alias bypassed lock")
	}
	if _, _, err := locks.AcquireLock("ws", map[string][]string{alias: {"1"}}, 60, LockActionAll); err == nil {
		t.Fatal("alias acquired duplicate lock")
	}
	if err := locks.CheckFeatureLock("ws", alias, "1", lock.LockID); err != nil {
		t.Fatal(err)
	}
	if !sameLockTypeName(key, sourceLockKey(svc, layer, "renamedroads")) || lockDisplayName(key) != "roads" {
		t.Fatal("presentation changed identity")
	}
	if sameLockTypeName(key, sourceLockKey(svc, &workspace.Layer{SourceLayer: "public.other"}, "roads")) {
		t.Fatal("unrelated source collided")
	}
	// The encoded source key is self-contained and survives JSON/catalog reload.
	identity, ok := decodeLockKey(key)
	if !ok || identity.Source == "" {
		t.Fatal("missing durable identity")
	}
	if !sameLockTypeName("legacy_public_name", alias) {
		t.Fatal("legacy upgrade must fail closed until expiry")
	}
}

func TestResourceIDUsesPublishedNameBeforeLegacySyntax(t *testing.T) {
	for _, tc := range []struct{ name, rid, want string }{
		{"roads_alias", "roads_alias.1", "1"}, {"public.roads_alias", "public.roads_alias.a.b", "a.b"},
		{"roads", "roads.a_b.c", "a_b.c"}, {"cite:RoadSegments", "cite_RoadSegments.1", "1"},
		{"uuid_roads", "uuid_roads.53357982-5ff0-4dfd-a351-931606645881", "53357982-5ff0-4dfd-a351-931606645881"},
	} {
		f, err := ParseFESFilter(`<Filter><ResourceId rid="` + tc.rid + `"/></Filter>`)
		if err != nil {
			t.Fatal(err)
		}
		_, args, _, err := CompileFES(f, FESCompileOptions{CollectionID: tc.name, StartParamIndex: 1})
		if err != nil || len(args) != 1 || args[0] != tc.want {
			t.Fatalf("%s: %v %v", tc.rid, args, err)
		}
	}
}
