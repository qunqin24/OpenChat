package llm

import "strings"

// Input modalities, spelled as the model catalogue spells them. Text is the
// baseline every model has and is not listed.
const (
	ModalityImage = "image"
	ModalityPDF   = "pdf"
)

// ModalityOf is the input modality a media type is sent to a model as, or ""
// for one this package does not know how to send.
func ModalityOf(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return ModalityImage
	case mediaType == "application/pdf":
		return ModalityPDF
	}
	return ""
}
