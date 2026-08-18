package handlers

import (
	"reflect"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
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

func TestBuildImportedActionAcceptsValidItem(t *testing.T) {
	item := actionImportItem{
		Name:        "Install Nginx",
		Description: "Installs and enables nginx",
		Category:    "packages",
		Script:      "#!/bin/bash\napt-get install -y nginx",
		Tags:        []string{"Nginx", "nginx"},
	}
	action, err := buildImportedAction(item)
	if err != nil {
		t.Fatalf("buildImportedAction: %v", err)
	}
	if action.Name != item.Name || action.Script != item.Script || action.Category != "packages" {
		t.Errorf("unexpected action: %+v", action)
	}
	if !reflect.DeepEqual(action.Tags, []string{"nginx"}) {
		t.Errorf("expected deduped/lowercased tags, got %v", action.Tags)
	}
}

func TestBuildImportedActionDefaultsCategoryToCustom(t *testing.T) {
	action, err := buildImportedAction(actionImportItem{Name: "n", Script: "echo hi"})
	if err != nil {
		t.Fatalf("buildImportedAction: %v", err)
	}
	if action.Category != "custom" {
		t.Errorf("expected category to default to custom, got %q", action.Category)
	}
}

func TestBuildImportedActionRejectsMissingFields(t *testing.T) {
	cases := []actionImportItem{
		{Name: "", Script: "echo hi"},
		{Name: "n", Script: ""},
	}
	for _, c := range cases {
		if _, err := buildImportedAction(c); err == nil {
			t.Errorf("expected error for %+v", c)
		}
	}
}

func TestBuildImportedActionRejectsInvalidCategory(t *testing.T) {
	_, err := buildImportedAction(actionImportItem{Name: "n", Script: "echo hi", Category: "not-a-real-category"})
	if err == nil {
		t.Fatal("expected error for invalid category")
	}
}

func TestBuildImportedActionRejectsInvalidParameters(t *testing.T) {
	_, err := buildImportedAction(actionImportItem{
		Name:   "n",
		Script: "echo hi",
		Parameters: []models.ActionParameter{
			{Name: "lowercase_name", Label: "Bad", Type: "string"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid parameter name")
	}
}

func TestBuildImportedActionNeverSetsBuiltinOrID(t *testing.T) {
	action, err := buildImportedAction(actionImportItem{Name: "n", Script: "echo hi"})
	if err != nil {
		t.Fatalf("buildImportedAction: %v", err)
	}
	if action.Builtin {
		t.Error("imported action must never be marked builtin")
	}
	if action.ID != 0 {
		t.Errorf("expected zero-value ID before insert, got %d", action.ID)
	}
}
