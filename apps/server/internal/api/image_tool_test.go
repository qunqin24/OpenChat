package api

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSplitImageRefs(t *testing.T) {
	handles, rest := splitImageRefs("<image-ref id=\"img_ab12cd34\"/>\n<image-ref id=\"img_0000ffff\"/>\nmake it blue")
	if !slices.Equal(handles, []string{"img_ab12cd34", "img_0000ffff"}) || rest != "make it blue" {
		t.Fatalf("%v %q", handles, rest)
	}
	if handles, rest := splitImageRefs("<image-ref id=\"nope\"/>\nx"); handles != nil || rest != "<image-ref id=\"nope\"/>\nx" {
		t.Fatalf("%v %q", handles, rest)
	}
	if got := personWords("<skill name=\"draw\"/>\n<image-ref id=\"img_ab12cd34\"/>\nbluer"); got != "bluer" {
		t.Fatalf("personWords = %q", got)
	}
}

func TestImageToolDefinitionWithAndWithoutQuality(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		def := imageTool{fixedQuality: fixed, fixedComplexity: fixed}.Definition()
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(def.Parameters, &schema); err != nil {
			t.Fatalf("fixed=%v: parameters are not JSON: %v", fixed, err)
		}
		_, has := schema.Properties["quality"]
		if has == fixed || strings.Contains(def.Description, "Set quality") == fixed {
			t.Fatalf("fixed=%v: quality offered = %v", fixed, has)
		}
		if _, has := schema.Properties["complexity"]; has == fixed || strings.Contains(def.Description, "Set complexity") == fixed {
			t.Fatalf("fixed=%v: complexity offered = %v", fixed, has)
		}
		if _, ok := schema.Properties["background"]; !ok {
			t.Fatalf("fixed=%v: background missing", fixed)
		}
	}
}
