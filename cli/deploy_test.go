package main

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	got := diff([]string{"a", "b", "c", "d"}, []string{"a", "c"})
	if want := []string{"b", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("diff = %v, want %v", got, want)
	}
}

func TestDiffEmptyOld(t *testing.T) {
	got := diff([]string{"x", "y"}, nil)
	if want := []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("diff with empty old = %v, want %v", got, want)
	}
}

func TestDiffNoNew(t *testing.T) {
	if got := diff([]string{"a"}, []string{"a"}); len(got) != 0 {
		t.Errorf("expected no new containers, got %v", got)
	}
}
