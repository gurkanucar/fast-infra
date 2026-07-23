package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

const (
	pgContainer    = "fast-infra-postgres-1"
	redisContainer = "fast-infra-redis-1"
)

// randPassword returns n random bytes as a hex string (2n chars). Hex keeps the
// password free of characters that would need escaping in SQL or a Redis ACL.
func randPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// pgProvisionSQL builds idempotent SQL that creates (or re-passwords) a
// least-privilege role owning only its own database. Password is hex, so it is
// safe inside the single-quoted literal.
func pgProvisionSQL(name, pw string) string {
	return fmt.Sprintf(`DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%[1]s') THEN
    CREATE ROLE "%[1]s" LOGIN PASSWORD '%[2]s';
  ELSE
    ALTER ROLE "%[1]s" WITH LOGIN PASSWORD '%[2]s';
  END IF;
END $$;
SELECT 'CREATE DATABASE "%[1]s" OWNER "%[1]s"'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '%[1]s')\gexec
`, name, pw)
}

// provisionPostgres creates the app's database and owning role, returning the
// generated password. Requires the postgres container to be running.
func provisionPostgres(name string) (string, error) {
	pw, err := randPassword(18)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("docker", "exec", "-i", pgContainer,
		"psql", "-v", "ON_ERROR_STOP=1", "-U", "postgres")
	cmd.Stdin = strings.NewReader(pgProvisionSQL(name, pw))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return pw, nil
}

// redisACLArgs builds the ACL SETUSER arguments scoping the user to name:* keys
// and channels.
func redisACLArgs(name, pw string) []string {
	return []string{"ACL", "SETUSER", name, "on", ">" + pw, "~" + name + ":*", "&" + name + ":*", "+@all"}
}

// provisionRedis creates a Redis ACL user scoped to the name:* prefix and
// persists it to the ACL file. Requires the redis container to run with
// --aclfile (see infra/docker-compose.yml).
func provisionRedis(name string) (string, error) {
	pw, err := randPassword(18)
	if err != nil {
		return "", err
	}
	args := append([]string{"exec", redisContainer, "redis-cli"}, redisACLArgs(name, pw)...)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if s := strings.TrimSpace(string(out)); err != nil || s != "OK" {
		return "", fmt.Errorf("ACL SETUSER: %s", s)
	}
	// Persist so the user survives a redis restart.
	out, err = exec.Command("docker", "exec", redisContainer, "redis-cli", "ACL", "SAVE").CombinedOutput()
	if s := strings.TrimSpace(string(out)); err != nil || s != "OK" {
		return "", fmt.Errorf("ACL SAVE (is redis using --aclfile?): %s", s)
	}
	return pw, nil
}
