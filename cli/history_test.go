package main

import "testing"

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []struct{ tag, status string }{
		{"v1", "success"}, {"v2", "failed"}, {"v3", "success"},
	} {
		if err := recordDeployment(dir, d.tag, d.status); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := readHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Fatalf("want 3 entries, got %d", len(hist))
	}
	if hist[0].Tag != "v1" || hist[2].Tag != "v3" {
		t.Errorf("append order wrong: %+v", hist)
	}
	if hist[1].Status != "failed" {
		t.Errorf("status not preserved: %+v", hist[1])
	}
	if hist[0].Time.IsZero() {
		t.Error("time not recorded")
	}
}

func TestReadHistoryMissing(t *testing.T) {
	dir := t.TempDir()
	hist, err := readHistory(dir)
	if err != nil {
		t.Fatalf("missing history should be (nil, nil): %v", err)
	}
	if hist != nil {
		t.Errorf("want nil, got %+v", hist)
	}
}

func TestPreviousSuccess(t *testing.T) {
	dir := t.TempDir()
	recordDeployment(dir, "v1", "success")
	recordDeployment(dir, "v2", "success")
	recordDeployment(dir, "v3", "failed")

	if prev, ok := previousSuccess(dir, "v2"); !ok || prev != "v1" {
		t.Errorf("previousSuccess(v2) = %q,%v want v1,true", prev, ok)
	}
	// v3 was the last (failed) tag; the newest success that isn't v3 is v2.
	if prev, ok := previousSuccess(dir, "v3"); !ok || prev != "v2" {
		t.Errorf("previousSuccess(v3) = %q,%v want v2,true", prev, ok)
	}
}

func TestPreviousSuccessNone(t *testing.T) {
	dir := t.TempDir()
	recordDeployment(dir, "v1", "success")
	if _, ok := previousSuccess(dir, "v1"); ok {
		t.Error("only successful tag equals current — expected no previous")
	}
}
