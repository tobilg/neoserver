package postgis

import "testing"

func TestSQLViewStructuralPolicy(t *testing.T) {
	for _, query := range []string{
		`SELECT id, ST_Transform(geom,4326) FROM public.roads`,
		`WITH x AS (SELECT id, geom FROM roads) SELECT * FROM x`,
		`SELECT 'DROP TABLE' AS description, created_at FROM roads`,
		`SELECT pg_catalog.lower(name), "st_area"/**/(geom) FROM roads`,
	} {
		if err := validateSQLStructure(query); err != nil {
			t.Errorf("allowed %s: %v", query, err)
		}
	}
	for _, query := range []string{
		`SELECT "pg_read_file"('/etc/passwd')`, `SELECT pg_read_file/**/('/etc/passwd')`,
		`SELECT pg_catalog."pg_read_binary_file"('/etc/passwd')`, `SELECT public.custom_function()`,
		`SELECT dblink('connection','query')`, `SELECT pg_sleep(100)`, `SELECT set_config('default_transaction_read_only','off',false)`,
		`SELECT * INTO new_table FROM roads`, `SELECT * FROM roads FOR UPDATE`,
		`WITH x AS (DELETE FROM roads RETURNING *) SELECT * FROM x`, `SELECT 1; SELECT 2`,
		`SELECT * FROM roads TABLESAMPLE custom_sampler(1)`,
	} {
		if err := validateSQLStructure(query); err == nil {
			t.Errorf("unsafe query accepted: %s", query)
		}
	}
}

func TestConnectionConfigPreservesCredentials(t *testing.T) {
	for _, password := range []string{"review password", `quote'\\value`, "#/@:?%&=", ""} {
		cfg := DefaultConfig()
		cfg.Host = "::1"
		cfg.User = "user name"
		cfg.Password = password
		cfg.Database = "db name"
		parsed, err := connectionConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.ConnConfig.Password != password || parsed.ConnConfig.User != cfg.User || parsed.ConnConfig.Database != cfg.Database || parsed.ConnConfig.Host != cfg.Host {
			t.Fatal("connection credential round trip failed")
		}
	}
	cfg := DefaultConfig()
	cfg.SSLMode = "invalid"
	cfg.Password = "secret-marker"
	if _, err := connectionConfig(cfg); err == nil {
		t.Fatal("invalid TLS mode accepted")
	}
}
