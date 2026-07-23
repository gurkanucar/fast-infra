package main

import (
	"strings"
	"testing"
)

func TestPgProvisionSQL(t *testing.T) {
	sql := pgProvisionSQL("blog", "deadbeef")
	for _, want := range []string{
		`CREATE ROLE "blog" LOGIN PASSWORD 'deadbeef'`,
		`ALTER ROLE "blog" WITH LOGIN PASSWORD 'deadbeef'`,
		`CREATE DATABASE "blog" OWNER "blog"`,
		`WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'blog')`,
		`\gexec`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q\n%s", want, sql)
		}
	}
}

func TestRedisACLArgs(t *testing.T) {
	got := redisACLArgs("blog", "s3cret")
	want := []string{"ACL", "SETUSER", "blog", "on", ">s3cret", "~blog:*", "&blog:*", "+@all"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRandPasswordUnique(t *testing.T) {
	a, err := randPassword(18)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := randPassword(18)
	if len(a) != 36 {
		t.Errorf("want 36 hex chars, got %d", len(a))
	}
	if a == b {
		t.Error("two passwords should differ")
	}
}

func TestRenderAppEnv(t *testing.T) {
	db, rd := "dbpw", "rdpw"

	plain := renderAppEnv("api", nil, nil)
	if !strings.Contains(plain, "postgres://postgres:CHANGE_ME@postgres:5432/api") {
		t.Errorf("plain DB line wrong:\n%s", plain)
	}
	if !strings.Contains(plain, "REDIS_URL=redis://redis:6379") {
		t.Errorf("plain Redis line wrong:\n%s", plain)
	}

	prov := renderAppEnv("api", &db, &rd)
	if !strings.Contains(prov, "postgres://api:dbpw@postgres:5432/api") {
		t.Errorf("provisioned DB line wrong:\n%s", prov)
	}
	if !strings.Contains(prov, "REDIS_URL=redis://api:rdpw@redis:6379") {
		t.Errorf("provisioned Redis line wrong:\n%s", prov)
	}

	// Mixed: DB provisioned, Redis not.
	mixed := renderAppEnv("api", &db, nil)
	if !strings.Contains(mixed, "postgres://api:dbpw@") || !strings.Contains(mixed, "REDIS_URL=redis://redis:6379") {
		t.Errorf("mixed env wrong:\n%s", mixed)
	}

	// The OTEL endpoint is always present.
	if !strings.Contains(plain, "OTEL_EXPORTER_OTLP_ENDPOINT=http://openobserve:5080/api/default") {
		t.Errorf("OTEL line missing:\n%s", plain)
	}
}
