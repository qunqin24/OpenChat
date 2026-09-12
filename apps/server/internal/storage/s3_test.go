package storage

import "testing"

func TestObjectURLPathStyle(t *testing.T) {
	s := &Store{bucket: "openchat", endpoint: "http://localhost:9000", pathSty: true}

	got := s.ObjectURL("uploads/2026/09/12/id.md")
	want := "http://localhost:9000/openchat/uploads/2026/09/12/id.md"
	if got != want {
		t.Errorf("ObjectURL = %q, want %q", got, want)
	}
}

func TestObjectURLVirtualHosted(t *testing.T) {
	s := &Store{bucket: "openchat", endpoint: "https://s3.example.com", pathSty: false}

	got := s.ObjectURL("uploads/a b.png")
	want := "https://openchat.s3.example.com/uploads/a%20b.png"
	if got != want {
		t.Errorf("ObjectURL = %q, want %q", got, want)
	}
}
