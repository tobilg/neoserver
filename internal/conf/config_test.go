package conf

import (
	"os"
	"testing"
)

func TestNormalizeBasePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:  "quoted empty",
			input: `""`,
			want:  "",
		},
		{
			name:  "simple path",
			input: "/api",
			want:  "/api",
		},
		{
			name:  "path without leading slash",
			input: "api",
			want:  "/api",
		},
		{
			name:  "path with trailing slash",
			input: "/api/",
			want:  "/api",
		},
		{
			name:  "path with multiple trailing slashes",
			input: "/api///",
			want:  "/api",
		},
		{
			name:  "quoted path",
			input: `"/api"`,
			want:  "/api",
		},
		{
			name:  "nested path",
			input: "/api/v1",
			want:  "/api/v1",
		},
		{
			name:  "path with whitespace",
			input: "  /api  ",
			want:  "/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBasePath(tt.input)
			if got != tt.want {
				t.Errorf("normalizeBasePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal config",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
			},
			wantErr: false,
		},
		{
			name: "invalid port - zero",
			cfg: Config{
				Server: Server{HttpPort: 0},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
			},
			wantErr: true,
			errMsg:  "invalid Server.HttpPort",
		},
		{
			name: "invalid port - negative",
			cfg: Config{
				Server: Server{HttpPort: -1},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
			},
			wantErr: true,
			errMsg:  "invalid Server.HttpPort",
		},
		{
			name: "invalid port - too high",
			cfg: Config{
				Server: Server{HttpPort: 70000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
			},
			wantErr: true,
			errMsg:  "invalid Server.HttpPort",
		},
		{
			name: "invalid paging - zero default",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 0, LimitMax: 100},
			},
			wantErr: true,
			errMsg:  "invalid Paging.LimitDefault",
		},
		{
			name: "invalid paging - max less than default",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 100, LimitMax: 50},
			},
			wantErr: true,
			errMsg:  "invalid Paging.LimitMax",
		},
		{
			name: "valid WMS config",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WMS:    WMS{Enabled: true, MaxWidth: 4096, MaxHeight: 4096, MaxPixels: 4096 * 4096},
			},
			wantErr: false,
		},
		{
			name: "invalid WMS - zero width",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WMS:    WMS{Enabled: true, MaxWidth: 0, MaxHeight: 4096, MaxPixels: 4096 * 4096},
			},
			wantErr: true,
			errMsg:  "invalid WMS.MaxWidth",
		},
		{
			name: "invalid WMS - zero height",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WMS:    WMS{Enabled: true, MaxWidth: 4096, MaxHeight: 0, MaxPixels: 4096 * 4096},
			},
			wantErr: true,
			errMsg:  "invalid WMS.MaxHeight",
		},
		{
			name: "valid WFS config",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS:    WFS{Enabled: true, MaxFeatures: 10000, DefaultCount: 100},
			},
			wantErr: false,
		},
		{
			name: "invalid WFS - zero max features",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS:    WFS{Enabled: true, MaxFeatures: 0, DefaultCount: 100},
			},
			wantErr: true,
			errMsg:  "invalid WFS.MaxFeatures",
		},
		{
			name: "invalid WFS - zero default count",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS:    WFS{Enabled: true, MaxFeatures: 10000, DefaultCount: 0},
			},
			wantErr: true,
			errMsg:  "invalid WFS.DefaultCount",
		},
		{
			name: "invalid WFS - default count exceeds max",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS:    WFS{Enabled: true, MaxFeatures: 100, DefaultCount: 200},
			},
			wantErr: true,
			errMsg:  "WFS.DefaultCount",
		},
		{
			name: "invalid WFS - incomplete export limits",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS: WFS{
					Enabled: true, MaxFeatures: 10000, DefaultCount: 100,
					MaxOutputBytes: 1024,
				},
			},
			wantErr: true,
			errMsg:  "WFS export limits",
		},
		{
			name: "invalid WFS - anonymous mutations with authentication",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				WFS: WFS{
					Enabled:                 true,
					AllowAnonymousMutations: true,
					MaxFeatures:             10000,
					DefaultCount:            100,
				},
				Auth: Auth{Enabled: true, Method: "apikey", ApiKey: "secret123"},
			},
			wantErr: true,
			errMsg:  "WFS.AllowAnonymousMutations requires Auth.Enabled=false",
		},
		{
			name: "valid auth - apikey",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth:   Auth{Enabled: true, Method: "apikey", ApiKey: "secret123"},
			},
			wantErr: false,
		},
		{
			name: "invalid auth - apikey without key",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth:   Auth{Enabled: true, Method: "apikey", ApiKey: ""},
			},
			wantErr: true,
			errMsg:  "Auth.ApiKey is required",
		},
		{
			name: "invalid auth - unknown method",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth:   Auth{Enabled: true, Method: "unknown"},
			},
			wantErr: true,
			errMsg:  "invalid Auth.Method",
		},
		{
			name: "valid auth - oidc",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth: Auth{
					Enabled: true,
					Method:  "oidc",
					OIDC: OIDCConfig{
						IssuerURL: "https://issuer.example.com",
						ClientID:  "client123",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid auth - oidc without issuer",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth: Auth{
					Enabled: true,
					Method:  "oidc",
					OIDC:    OIDCConfig{ClientID: "client123"},
				},
			},
			wantErr: true,
			errMsg:  "Auth.OIDC.IssuerURL is required",
		},
		{
			name: "invalid auth - oidc without client id",
			cfg: Config{
				Server: Server{HttpPort: 9000},
				Paging: Paging{LimitDefault: 10, LimitMax: 100},
				Auth: Auth{
					Enabled: true,
					Method:  "oidc",
					OIDC:    OIDCConfig{IssuerURL: "https://issuer.example.com"},
				},
			},
			wantErr: true,
			errMsg:  "Auth.OIDC.ClientID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseAuthUsers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "empty string",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "single user",
			input: "user1:pass1",
			want:  map[string]string{"user1": "pass1"},
		},
		{
			name:  "multiple users",
			input: "user1:pass1,user2:pass2,user3:pass3",
			want:  map[string]string{"user1": "pass1", "user2": "pass2", "user3": "pass3"},
		},
		{
			name:  "with whitespace",
			input: " user1 : pass1 , user2 : pass2 ",
			want:  map[string]string{"user1": "pass1", "user2": "pass2"},
		},
		{
			name:  "empty pair skipped",
			input: "user1:pass1,,user2:pass2",
			want:  map[string]string{"user1": "pass1", "user2": "pass2"},
		},
		{
			name:  "invalid format skipped",
			input: "user1:pass1,invalid,user2:pass2",
			want:  map[string]string{"user1": "pass1", "user2": "pass2"},
		},
		{
			name:  "password with colon",
			input: "user1:pass:word",
			want:  map[string]string{"user1": "pass:word"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAuthUsers(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseAuthUsers(%q) returned %d users, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseAuthUsers(%q)[%q] = %q, want %q", tt.input, k, got[k], v)
				}
			}
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Test loading with defaults only (no config file)
	cfg, err := Load("", false, false)
	if err != nil {
		t.Fatalf("unexpected error loading defaults: %v", err)
	}

	// Check some default values
	if cfg.Server.HttpPort != 9000 {
		t.Errorf("expected default HttpPort 9000, got %d", cfg.Server.HttpPort)
	}
	if cfg.Paging.LimitDefault != 10 {
		t.Errorf("expected default LimitDefault 10, got %d", cfg.Paging.LimitDefault)
	}
	if cfg.Paging.CountTimeoutMS != 5000 {
		t.Errorf("expected default CountTimeoutMS 5000, got %d", cfg.Paging.CountTimeoutMS)
	}
	if cfg.Cache.Enabled != true {
		t.Errorf("expected Cache.Enabled true by default")
	}
	if cfg.PersistentCache.Enabled {
		t.Error("expected PersistentCache.Enabled false by default")
	}
	if cfg.PersistentCache.DatabasePath != "./data/tile-cache.duckdb" {
		t.Errorf("expected default persistent cache database path, got %q", cfg.PersistentCache.DatabasePath)
	}
	if cfg.WFS.AllowAnonymousMutations {
		t.Error("expected WFS.AllowAnonymousMutations false by default")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	// Set environment variable
	os.Setenv("NEOSRV_SERVER_HTTPPORT", "8080")
	defer os.Unsetenv("NEOSRV_SERVER_HTTPPORT")

	cfg, err := Load("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.HttpPort != 8080 {
		t.Errorf("expected HttpPort 8080 from env, got %d", cfg.Server.HttpPort)
	}
}

func TestLoad_DebugFlag(t *testing.T) {
	cfg, err := Load("", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Server.Debug {
		t.Error("expected Debug=true when debug flag is set")
	}
}

func TestLoad_DevelFlag(t *testing.T) {
	cfg, err := Load("", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Server.Devel {
		t.Error("expected Devel=true when devel flag is set")
	}
}

func TestLoad_DatabaseURLEnv(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	defer os.Unsetenv("DATABASE_URL")

	cfg, err := Load("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Database.DatabaseURL != "postgres://user:pass@localhost/db" {
		t.Errorf("expected DATABASE_URL to be set, got %q", cfg.Database.DatabaseURL)
	}
}

func TestLoad_AuthUsersEnv(t *testing.T) {
	os.Setenv("AUTH_USERS", "admin:secret,user:pass")
	defer os.Unsetenv("AUTH_USERS")

	cfg, err := Load("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Auth.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(cfg.Auth.Users))
	}
	if cfg.Auth.Users["admin"] != "secret" {
		t.Errorf("expected admin:secret, got %q", cfg.Auth.Users["admin"])
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
