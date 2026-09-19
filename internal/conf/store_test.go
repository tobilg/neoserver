package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdminStoreConfigPrecedence(t *testing.T) {
	t.Setenv("NEOSRV_STORE_PATH", "")
	t.Setenv("NEOSRV_STORE_KEY", "abc123")
	t.Setenv("NEOSRV_STORE_ENCRYPTIONKEY", "")
	config := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(config, []byte("[Store]\nPath = '/catalog/from-config.db'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ env, override, want string }{{"", "", "/catalog/from-config.db"}, {"/catalog/from-env.db", "", "/catalog/from-env.db"}, {"/catalog/from-env.db", "/catalog/explicit.db", "/catalog/explicit.db"}} {
		t.Setenv("NEOSRV_STORE_PATH", tc.env)
		cfg, err := LoadStore(config, tc.override)
		if err != nil || cfg.Path != tc.want || cfg.EncryptionKey != "abc123" {
			t.Fatalf("unexpected store config: path=%s err=%v", cfg.Path, err)
		}
	}
	if _, err := LoadStore(config+".missing", ""); err == nil {
		t.Fatal("missing explicit config ignored")
	}
}
