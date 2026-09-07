package handlers

import (
	"encoding/json"
	"github.com/dovod-app/app/internal/domain"
)

// The bodies the write endpoints accept, as named types.
//
// They were anonymous structs inside each handler, which is fine for decoding
// and useless for anything else: the OpenAPI document had to describe them from
// memory, and when it was first written that way half of them were wrong —
// `type` for `entry_type`, a `description` on a task update that does not exist,
// a `question` field on the answer route that is not read. Naming them lets the
// document be generated from the same declaration the decoder uses, so the two
// cannot disagree.
//
// The `doc` tags are what huma turns into field descriptions. A field with
// nothing to say does not need one.

// CreateResearchRequest is the body of POST /api/researches.
type CreateResearchRequest struct {
	Name        string                 `json:"name" doc:"Required."`
	Description string                 `json:"description,omitempty"`
	Goal        string                 `json:"goal,omitempty" doc:"What the research is trying to find out."`
	Tags        []string               `json:"tags,omitempty"`
	TeamID      string                 `json:"team_id,omitempty" doc:"Which team owns it. Defaults to the caller's personal team."`
	Sections    []CreateSectionRequest `json:"sections,omitempty" doc:"Sections to start with. A research created without any has none until one is added."`
}

// CreateSectionRequest is a section in a create call, and the body of
// POST /api/researches/{id}/sections.
type CreateSectionRequest struct {
	Name        string `json:"name" doc:"Required. Machine name, used in exports and paths."`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Position    int    `json:"position,omitempty"`
}

// UpdateResearchRequest is the body of PUT /api/researches/{id}. Every field is
// optional; an omitted one is left alone.
type UpdateResearchRequest struct {
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Goal        *string         `json:"goal,omitempty"`
	Status      *string         `json:"status,omitempty" enum:"active,completed,archived" doc:"An archived research is hidden from the default listing, not deleted."`
	Instruction json.RawMessage `json:"instruction,omitempty" hidden:"true"`
	Tags        []string        `json:"tags,omitempty"`
	Memory      json.RawMessage `json:"memory,omitempty" hidden:"true"`
	AddMemory   *string         `json:"add_memory,omitempty" doc:"Atomically appends one note. Edit and delete existing notes through the per-item memory routes."`
	SessionID   string          `json:"session_id,omitempty" doc:"Research session UUID or SS code for the appended note."`
}

// UpdateSectionRequest is the body of PUT /api/sections/{sectionId}.
type UpdateSectionRequest struct {
	DisplayName *string             `json:"display_name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Status      *string             `json:"status,omitempty" enum:"draft,active,completed"`
	Position    *int                `json:"position,omitempty"`
	Instruction *string             `json:"instruction,omitempty" doc:"How to write a document in this section — three to six lines of imperatives, read before every document filed here. At most 500 characters, refused rather than truncated. Send \"\" to remove it."`
	FieldSpec   *[]domain.FieldSpec `json:"field_spec,omitempty" doc:"The typed fields documents in this section must carry. GET /api/metadata/schema describes what is allowed."`
}

// CreateEntryRequest is the body of POST /api/entries.
type CreateEntryRequest struct {
	ResearchID  string         `json:"research_id" doc:"Required."`
	SectionID   string         `json:"section_id" doc:"Required."`
	SessionID   string         `json:"session_id,omitempty" doc:"Which interview session produced this document, if any."`
	EntryType   string         `json:"entry_type,omitempty" enum:"markdown,blocks" doc:"A blocks document carries structured content; content is then a block document as JSON."`
	Content     string         `json:"content" doc:"Required. Markdown, or a block document as JSON when entry_type is blocks."`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status,omitempty" enum:"draft,active,completed,archived"`
	Tags        []string       `json:"tags,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty" doc:"Values for the typed fields the section declares."`
}

// TextReplaceRequest is the smallest edit a document update can carry: one
// literal substitution, for a change that does not need the whole content.
type TextReplaceRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// UpdateEntryRequest is the body of PUT /api/entries/{id}.
type UpdateEntryRequest struct {
	EntryType   *string             `json:"entry_type,omitempty" enum:"markdown,blocks"`
	Title       *string             `json:"title,omitempty"`
	Content     *string             `json:"content,omitempty"`
	Description *string             `json:"description,omitempty"`
	Status      *string             `json:"status,omitempty" enum:"draft,active,completed,archived"`
	Tags        []string            `json:"tags,omitempty"`
	TextReplace *TextReplaceRequest `json:"text_replace,omitempty" doc:"Replace one literal string instead of sending the whole content."`
	SessionID   *string             `json:"session_id,omitempty"`
	// A pointer so an omitted map leaves the values alone and an empty one
	// clears them, which is the same distinction the MCP tool draws.
	Metadata        *map[string]any `json:"metadata,omitempty" doc:"Omitted leaves the values alone; an empty object clears them."`
	AllowIncomplete bool            `json:"allow_incomplete,omitempty" doc:"Write even though the section's required fields are not all filled in."`
}

// PatchOp is one operation in a document patch.
type PatchOp struct {
	Op      string         `json:"op" enum:"update,insert,delete,move,set_state"`
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	After   string         `json:"after,omitempty"`
	Before  string         `json:"before,omitempty"`
	At      string         `json:"at,omitempty"`
	Item    string         `json:"item,omitempty"`
	Checked *bool          `json:"checked,omitempty"`
}

// PatchEntryRequest is the body of POST /api/entries/{id}/patch.
type PatchEntryRequest struct {
	Rev string    `json:"rev" doc:"The revision the patch was computed against — the revision field of GET /api/entries/{id}. A mismatch is a conflict rather than a silent overwrite."`
	Ops []PatchOp `json:"ops"`
}

// CreateTaskRequest is the body of POST /api/tasks.
type CreateTaskRequest struct {
	ResearchID  string `json:"research_id" doc:"Required."`
	Title       string `json:"title" doc:"Required."`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty" enum:"high,medium,low"`
}

// UpdateTaskRequest is the body of PUT /api/tasks/{id}.
type UpdateTaskRequest struct {
	Title    *string `json:"title,omitempty"`
	Status   *string `json:"status,omitempty" enum:"pending,in_progress,completed,blocked"`
	Priority *string `json:"priority,omitempty" enum:"high,medium,low"`
	Result   *string `json:"result,omitempty" doc:"What the task produced."`
}

// CreateQuestionRequest is a question in a session create call.
type CreateQuestionRequest struct {
	Text      string `json:"text"`
	Area      string `json:"area,omitempty"`
	Rationale string `json:"rationale,omitempty" doc:"Why this is worth asking."`
	Priority  string `json:"priority,omitempty"`
	ParentID  string `json:"parent_id,omitempty" doc:"The question this one follows up on."`
	Position  int    `json:"position,omitempty"`
}

// CreateSessionRequest is the body of POST /api/sessions.
type CreateSessionRequest struct {
	ResearchID string                  `json:"research_id" doc:"Required."`
	Title      string                  `json:"title" doc:"Required."`
	Focus      string                  `json:"focus,omitempty" doc:"What this session is about."`
	Questions  []CreateQuestionRequest `json:"questions,omitempty"`
}

// UpdateSessionRequest is the body of PUT /api/sessions/{id}.
type UpdateSessionRequest struct {
	Title   *string `json:"title,omitempty"`
	Focus   *string `json:"focus,omitempty"`
	Status  *string `json:"status,omitempty" enum:"active,completed"`
	Notes   *string `json:"notes,omitempty" doc:"Replaces the notes. Working process; never served through a share link."`
	AddNote *string `json:"add_note,omitempty" doc:"Appends one note instead of replacing them."`
}

// UpdateQuestionRequest is the body of PUT /api/questions/{questionId}.
//
// The question text is not among the fields: a question is edited by the
// session that asked it, and this route exists for answering.
type UpdateQuestionRequest struct {
	Status *string `json:"status,omitempty" enum:"pending,answered,deferred,skipped" doc:"A value outside this set is stored and then appears in no bucket when the session is read, so the question is silently lost from the interview."`
	Answer *string `json:"answer,omitempty" doc:"May carry [[...]] cross-references like any other text."`
}

// AddQuestionRequest is one question appended to a running session.
type AddQuestionRequest struct {
	Text     string `json:"text"`
	Area     string `json:"area,omitempty"`
	Priority string `json:"priority,omitempty"`
}

// AddQuestionsRequest is the body of POST /api/sessions/{id}/questions.
type AddQuestionsRequest struct {
	Questions []AddQuestionRequest `json:"questions" doc:"At least one."`
}

// Memory bodies are shared by the handlers and the generated API reference.
type AddMemoryRequest struct {
	Text      string `json:"text" minLength:"1" doc:"Nonempty note, at most 64000 UTF-8 bytes. Author is determined from authentication."`
	SessionID string `json:"session_id,omitempty" doc:"Optional research session UUID or SS code."`
}

type UpdateMemoryRequest struct {
	Text    string `json:"text" minLength:"1" doc:"New nonempty text. Creation provenance is preserved."`
	Version int    `json:"version" minimum:"1" doc:"Current item version. A stale version returns 409."`
}

type BulkDeleteMemoryRequest struct {
	IDs []string `json:"ids" minItems:"1" maxItems:"500" doc:"Explicitly selected memory item UUIDs. Later appends are untouched."`
}
