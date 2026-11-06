package testutil

import (
	"strings"
	"testing"
)

func TestConfigWithSearchPath_URL(t *testing.T) {
	t.Parallel()
	cfg, err := configWithSearchPath("postgres://u:p@localhost:5432/db?sslmode=disable", "md_demo_1")
	if err != nil {
		t.Fatalf("configWithSearchPath: %v", err)
	}
	if got := cfg.RuntimeParams["search_path"]; got != "md_demo_1" {
		t.Fatalf("search_path=%q", got)
	}
	if cfg.Host != "localhost" || cfg.Database != "db" || cfg.User != "u" {
		t.Fatalf("host/db/user = %q/%q/%q", cfg.Host, cfg.Database, cfg.User)
	}
}

func TestConfigWithSearchPath_URLPreservesQuery(t *testing.T) {
	t.Parallel()
	cfg, err := configWithSearchPath(
		"postgresql://u:p@127.0.0.1:5432/shopanda?sslmode=disable&application_name=mdtest",
		"md_x",
	)
	if err != nil {
		t.Fatalf("configWithSearchPath: %v", err)
	}
	if cfg.RuntimeParams["search_path"] != "md_x" {
		t.Fatalf("search_path=%q", cfg.RuntimeParams["search_path"])
	}
	if cfg.Database != "shopanda" || cfg.Host != "127.0.0.1" {
		t.Fatalf("host/db = %q/%q", cfg.Host, cfg.Database)
	}
	// application_name is a libpq RuntimeParam when present in the URL.
	if cfg.RuntimeParams["application_name"] != "mdtest" {
		t.Fatalf("application_name=%q want mdtest (must not clobber URL query params)", cfg.RuntimeParams["application_name"])
	}
}

func TestConfigWithSearchPath_KeywordDSN(t *testing.T) {
	t.Parallel()
	cfg, err := configWithSearchPath(
		"host=localhost port=5432 user=shopanda password=changeme dbname=shopanda sslmode=disable",
		"md_kw_1",
	)
	if err != nil {
		t.Fatalf("keyword DSN: %v", err)
	}
	if cfg.RuntimeParams["search_path"] != "md_kw_1" {
		t.Fatalf("search_path=%q", cfg.RuntimeParams["search_path"])
	}
	if cfg.Host != "localhost" || cfg.User != "shopanda" || cfg.Database != "shopanda" {
		t.Fatalf("parsed keyword DSN incorrectly: host=%q user=%q db=%q", cfg.Host, cfg.User, cfg.Database)
	}
}

func TestConfigWithSearchPath_RejectsEmptyAndGarbage(t *testing.T) {
	t.Parallel()
	if _, err := configWithSearchPath("", "s"); err == nil {
		t.Fatal("empty dsn: want error")
	}
	if _, err := configWithSearchPath("postgres://u@localhost/db", ""); err == nil {
		t.Fatal("empty schema: want error")
	}
	_, err := configWithSearchPath("://not-a-dsn", "s")
	if err == nil || !strings.Contains(err.Error(), "parse DSN") {
		t.Fatalf("garbage dsn: err=%v", err)
	}
}
