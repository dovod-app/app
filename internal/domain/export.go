package domain

import "time"

// ExportData is the portable representation of a complete research.
// It contains all data needed to recreate a research on another server.
type ExportData struct {
	Version    int            `json:"version"`
	ExportedAt time.Time      `json:"exported_at"`
	Research   ExportResearch `json:"research"`
}

type ExportResearch struct {
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	Goal          string               `json:"goal"`
	Status        ResearchStatus       `json:"status"`
	Instruction   string               `json:"instruction,omitempty"` // v1 import only; never populated on export
	Memory        Memory               `json:"memory,omitempty"`
	PrivateSkills []ExportPrivateSkill `json:"private_skills,omitempty"`
	Tags          []string             `json:"tags,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`

	Sections []ExportSection `json:"sections"`
	Sessions []ExportSession `json:"sessions,omitempty"`
	Tasks    []ExportTask    `json:"tasks,omitempty"`
	Roadmaps []ExportRoadmap `json:"roadmaps,omitempty"`
}

// ExportPrivateSkill carries research-owned methodology without local owner
// IDs. Detached skills also travel so importing a backup cannot lose their text.
type ExportPrivateSkill struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Body         string `json:"body"`
	NeedsTrigger bool   `json:"needs_trigger,omitempty"`
	Attached     bool   `json:"attached"`
}

type ExportSection struct {
	Name        string        `json:"name"`
	DisplayName string        `json:"display_name,omitempty"`
	Description string        `json:"description,omitempty"`
	Status      SectionStatus `json:"status"`
	Position    int           `json:"position"`
	// Instruction travels for the same reason FieldSpec below does: a research
	// that comes back from a dump without its writing conventions has its next
	// eighteen documents written from scratch again. It is never in a share's
	// export — the sections are redacted before the dump is built.
	Instruction string `json:"instruction,omitempty"`
	// FieldSpec travels so an imported research keeps the declaration its
	// documents were written against, rather than arriving as a pile of values
	// nothing explains.
	FieldSpec []FieldSpec `json:"field_spec,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`

	Entries []ExportEntry `json:"entries,omitempty"`
}

type ExportEntry struct {
	Title string `json:"title"`
	// Type is empty in exports written before entry types existed; import reads
	// that as markdown, which is what those entries were.
	Type        EntryType   `json:"entry_type,omitempty"`
	Content     string      `json:"content"`
	Description string      `json:"description,omitempty"`
	Status      EntryStatus `json:"status"`
	Tags        []string    `json:"tags,omitempty"`
	// Metadata travels with the document. On import it is validated against the
	// target section's declaration, which may be a different one — a portable
	// dump carries the values, not the authority that collected them.
	Metadata    map[string]any `json:"metadata,omitempty"`
	SessionCode string         `json:"session_code,omitempty"` // links to ExportSession.Code
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	// Annotations travel with the document they mark. They are working process
	// and appear in no other export — not the markdown one, not the vault — but
	// a portable dump is a move, and a move that drops the queue silently
	// discards the only record of what a person did not believe.
	Annotations []ExportAnnotation `json:"annotations,omitempty"`
}

// ExportAnnotation is one mark, carried without the identities that only mean
// something on the server it came from.
//
// What is deliberately absent: the id and code (reassigned on arrival), the
// user (an account on another server), the anchored revision (history does not
// travel, so every imported document starts at revision 1), and the task link
// (a task id is not portable). The anchor itself survives because it never was
// an id: a block id lives inside the document's own content, and the quote is
// the proof either way.
type ExportAnnotation struct {
	BlockID    string           `json:"block_id,omitempty"`
	Quote      Quote            `json:"quote"`
	Kind       AnnotationKind   `json:"kind"`
	Body       string           `json:"body,omitempty"`
	AuthorKind AuthorKind       `json:"author_kind,omitempty"`
	Status     AnnotationStatus `json:"status"`
	Resolution string           `json:"resolution,omitempty"`
	Rejections []Rejection      `json:"rejections,omitempty"`
	Attempts   int              `json:"attempts,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

type ExportSession struct {
	Code      string        `json:"code"` // for entry->session linking
	Title     string        `json:"title"`
	Focus     string        `json:"focus,omitempty"`
	Status    SessionStatus `json:"status"`
	Notes     string        `json:"notes,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`

	Questions []ExportQuestion `json:"questions,omitempty"`
}

type ExportQuestion struct {
	Text      string         `json:"text"`
	Area      string         `json:"area,omitempty"`
	Rationale string         `json:"rationale,omitempty"`
	Priority  Priority       `json:"priority"`
	Status    QuestionStatus `json:"status"`
	Answer    string         `json:"answer,omitempty"`
	Position  int            `json:"position"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`

	Children []ExportQuestion `json:"children,omitempty"`
}

type ExportTask struct {
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	Priority    Priority   `json:"priority"`
	Result      string     `json:"result,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ExportRoadmap struct {
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Statuses    []string      `json:"statuses,omitempty"`
	Status      RoadmapStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`

	Nodes []ExportRoadmapNode `json:"nodes,omitempty"`
	Edges []ExportRoadmapEdge `json:"edges,omitempty"`
}

type ExportRoadmapNode struct {
	Code        string    `json:"code"` // for edge/parent references
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	NodeType    string    `json:"node_type"`
	Status      string    `json:"status,omitempty"`
	PositionX   float64   `json:"position_x"`
	PositionY   float64   `json:"position_y"`
	ParentCode  string    `json:"parent_code,omitempty"` // links to another node's Code
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ExportRoadmapEdge struct {
	SourceCode string `json:"source_code"` // node Code
	TargetCode string `json:"target_code"` // node Code
	Label      string `json:"label,omitempty"`
	EdgeType   string `json:"edge_type"`
}
