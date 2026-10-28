package llm

import "testing"

func TestFileText(t *testing.T) {
	if text, ok := FileText([]byte("\xef\xbb\xbfhello 你好")); !ok || text != "hello 你好" {
		t.Errorf("FileText = %q %v", text, ok)
	}
	for _, data := range []string{"\xff\xfe", "a\x00b"} {
		if _, ok := FileText([]byte(data)); ok {
			t.Errorf("%q read as text", data)
		}
	}
}

func TestFileBlock(t *testing.T) {
	got := FileBlock(`a "b" <c>.txt`, "line\n\n")
	want := "\n\n<file name=\"a &quot;b&quot; &lt;c>.txt\">\nline\n</file>"
	if got != want {
		t.Errorf("FileBlock = %q, want %q", got, want)
	}
}

func TestEstimateTextAttachment(t *testing.T) {
	text := AttachmentPart{MediaType: TextMediaType, Size: 30_000}
	if got := EstimatePart(text); got != 10_000+MessageOverhead {
		t.Errorf("a 30 KB text file = %d tokens", got)
	}
	if got := EstimatePart(AttachmentPart{MediaType: "image/png", Size: 30_000}); got != AttachmentTokens {
		t.Errorf("an image = %d tokens", got)
	}
}
