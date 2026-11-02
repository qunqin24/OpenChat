package skills

import (
	"archive/zip"
	"bytes"
)

// Zip is a skill's files as an archive holding its folder, named for the skill,
// as the Agent Skills format lays one out: the file Unpack reads back, and that
// Open WebUI and Claude import.
func Zip(name string, files []File) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		entry, err := w.Create(name + "/" + f.Path)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write([]byte(f.Content)); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
