package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestAdminStoreCLIProcess(t *testing.T) {
	if os.Getenv("NEOSRV_TEST_ADMIN_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"neoserver"}, os.Args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing helper command")
}

func TestCLIFailsClosedOnFutureCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	catalog, _, err := store.Init(store.Config{Path: path, EncryptionKey: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	catalog.Close()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ATTACH '" + strings.ReplaceAll(path, "'", "''") + "' AS catalog (ENCRYPTION_KEY 'abc123'); INSERT INTO catalog.schema_info(version) VALUES (999)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("NEOSRV_STORE_KEY", "abc123")
	t.Setenv("NEOSRV_STORE_PATH", path)
	for _, args := range [][]string{{"serve"}, {"create-token", "--role", "super_admin"}, {"rotate-signing-key", "--force"}} {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestAdminStoreCLIProcess$", "--"}, args...)...)
		cmd.Dir = filepath.Dir(path)
		cmd.Env = append(os.Environ(), "NEOSRV_TEST_ADMIN_PROCESS=1")
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "newer than this binary supports") {
			t.Fatalf("%v: %v %s", args, err, output)
		}
	}
}

func TestAdministrationTargetsConfiguredCatalog(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "configured.db")
	other := filepath.Join(root, "other.db")
	t.Setenv("NEOSRV_STORE_KEY", "abc123")
	t.Setenv("NEOSRV_STORE_PATH", first)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestAdminStoreCLIProcess$", "--"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "NEOSRV_TEST_ADMIN_PROCESS=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v %s", args[0], err, output)
		}
	}
	run("init")
	run("init", "--store-path", other)
	run("create-token", "--role", "super_admin")
	run("add-claim-mapping", "--claim", "groups", "--value", "operators", "--role", "admin")
	run("rotate-signing-key", "--force")
	for _, tc := range []struct {
		path string
		want int
	}{{first, 1}, {other, 0}} {
		catalog, err := store.Open(store.Config{Path: tc.path, EncryptionKey: "abc123"})
		if err != nil {
			t.Fatal(err)
		}
		mappings, err := catalog.ListClaimMappings(context.Background(), "*")
		catalog.Close()
		if err != nil || len(mappings) != tc.want {
			t.Fatalf("wrong catalog mapping count: %d %v", len(mappings), err)
		}
	}
}
