package api

import (
	"slices"
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
