package wfs

import (
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

func TestLockStoreQuotasAndExpiryClamp(t *testing.T) {
	store := NewLockStore(10, 1, 1, 2)
	lock, _, err := store.AcquireLockOwned("ws", map[string][]string{"roads": {"1", "2"}}, 100, LockActionAll, "user")
	if err != nil {
		t.Fatal(err)
	}
	if lock.ExpiresAt.Sub(lock.CreatedAt).Seconds() > 10.1 {
		t.Fatalf("expiry was not clamped")
	}
	if _, _, err := store.AcquireLockOwned("ws", map[string][]string{"roads": {"3"}}, 1, LockActionAll, "user"); err == nil {
		t.Fatal("expected lock quota error")
	}
	store = NewLockStore(10, 10, 10, 1)
	if _, _, err := store.AcquireLockOwned("ws", map[string][]string{"roads": {"1", "2"}}, 1, LockActionAll, "user"); err == nil {
		t.Fatal("expected feature quota error")
	}
}

func TestLockStoreTreatsQualifiedAndLocalTypeNamesAsSameFeatureType(t *testing.T) {
	store := NewLockStore(60, 10, 10, 10)
	if _, _, err := store.AcquireLockOwned("ws", map[string][]string{"cite:Autos": {"4"}}, 60, LockActionAll, "user"); err != nil {
		t.Fatal(err)
	}

	if !store.IsFeatureLocked("ws", "Autos", "4") {
		t.Fatal("local type name did not find lock acquired with a qualified type name")
	}
	if _, _, err := store.AcquireLockOwned("ws", map[string][]string{"Autos": {"4"}}, 60, LockActionAll, "user"); err == nil {
		t.Fatal("expected duplicate lock through local type name to fail")
	}
}

func TestPropertyXMLNameCollision(t *testing.T) {
	_, err := propertyXMLNames(&datasource.LayerInfo{Properties: []datasource.PropertyInfo{{Name: "a b"}, {Name: "a:b"}}})
	if err == nil {
		t.Fatal("expected XML property-name collision")
	}
}
