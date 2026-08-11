package handlers

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTagsTrimsLowercasesAndDedupes(t *testing.T) {
	got, err := normalizeTags([]string{" Docker ", "docker", "Compose", "", "  "})
	if err != nil {
		t.Fatalf("normalizeTags: %v", err)
	}
	want := []string{"docker", "compose"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeTagsRejectsTooManyTags(t *testing.T) {
	tags := make([]string, 11)
	for i := range tags {
		tags[i] = "tag"
	}
	_, err := normalizeTags(tags)
	if err == nil {
		t.Fatal("expected an error for more than 10 tags")
	}
}

func TestNormalizeTagsRejectsOverlongTag(t *testing.T) {
	_, err := normalizeTags([]string{strings.Repeat("a", 31)})
	if err == nil {
		t.Fatal("expected an error for a tag over 30 characters")
	}
}

func TestNormalizeTagsHandlesEmptyInput(t *testing.T) {
	got, err := normalizeTags(nil)
	if err != nil {
		t.Fatalf("normalizeTags(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no tags, got %v", got)
	}
}
