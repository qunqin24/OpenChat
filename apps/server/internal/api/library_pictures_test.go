package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A picture the image tool made and the person deleted from the library is
// reported on the tool row that held it, so the reply shows where it was.
func TestRemovedPicturesOnToolRows(t *testing.T) {
	kept, gone := uuid.New(), uuid.New()
	details, err := json.Marshal(ImageToolDetails{Prompt: "an apple", Images: []ImageToolPicture{
		{ID: kept, Handle: "img_a", MediaType: "image/png"},
		{ID: gone, Handle: "img_b", MediaType: "image/webp"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	row := sqlc.Message{Role: sqlc.MessageRoleTool, Parts: agent.EncodeParts([]llm.Part{
		llm.ToolResultPart{CallID: "c1", Name: toolImageGenerate, Status: llm.ToolSucceeded, Details: details},
		// Another tool's details are never read as pictures.
		llm.ToolResultPart{CallID: "c2", Name: "web_search", Status: llm.ToolSucceeded, Details: details},
	})}

	removed := removedAttachments(row, []Attachment{{Id: kept}})
	if len(removed) != 1 || removed[0].Id != gone || removed[0].Filename != "img_b" || removed[0].ContentType != "image/webp" {
		t.Fatalf("removed = %+v", removed)
	}
	if got := removedAttachments(row, []Attachment{{Id: kept}, {Id: gone}}); len(got) != 0 {
		t.Errorf("both still there, removed = %+v", got)
	}
	row.Role = sqlc.MessageRoleAssistant
	if got := removedAttachments(row, nil); len(got) != 0 {
		t.Errorf("an assistant row reported %+v", got)
	}
}
