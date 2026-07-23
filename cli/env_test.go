package main

import (
	"os"
	"testing"
)

func TestEnvSetUpsertPreservesComments(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(envPath(dir), []byte("# comment\nFOO=1\nBAR=2\n"), 0o600)
	if err := envSet(dir, "app", []string{"BAR=99", "BAZ=3"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(envPath(dir))
	want := "# comment\nFOO=1\nBAR=99\nBAZ=3\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEnvSetValueWithEquals(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(envPath(dir), []byte(""), 0o600)
	// A value containing '=' (e.g. a base64 token) must survive intact.
	if err := envSet(dir, "app", []string{"TOKEN=ab=cd=="}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(envPath(dir))
	if string(got) != "TOKEN=ab=cd==\n" {
		t.Errorf("got %q", got)
	}
}

func TestEnvSetInvalidKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(envPath(dir), []byte(""), 0o600)
	if err := envSet(dir, "app", []string{"1BAD=x"}); err == nil {
		t.Error("expected error for key starting with a digit")
	}
	if err := envSet(dir, "app", []string{"NOEQUALS"}); err == nil {
		t.Error("expected error for missing '='")
	}
}

func TestEnvUnset(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(envPath(dir), []byte("A=1\nB=2\nC=3\n"), 0o600)
	if err := envUnset(dir, "app", []string{"B"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(envPath(dir))
	if string(got) != "A=1\nC=3\n" {
		t.Errorf("got %q", got)
	}
	if err := envUnset(dir, "app", []string{"ZZZ"}); err == nil {
		t.Error("expected error removing a key that is not set")
	}
}
