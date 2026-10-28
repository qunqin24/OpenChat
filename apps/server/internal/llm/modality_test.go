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
	text := AttachmentPart{MediaType: TextMediaType, Size: 12_000}
	if got := EstimatePart(text); got != 4_000+MessageOverhead {
		t.Errorf("a 12 KB text file = %d tokens", got)
	}
	// A long one goes as a card.
	long := AttachmentPart{MediaType: TextMediaType, Size: 200_000}
	if got := EstimatePart(long); got != FileCardBytes/3+MessageOverhead {
		t.Errorf("a 200 KB text file = %d tokens", got)
	}
	if got := EstimatePart(AttachmentPart{MediaType: "image/png", Size: 30_000}); got != AttachmentTokens {
		t.Errorf("an image = %d tokens", got)
	}
}
