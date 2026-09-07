# Conducting Research

Step-by-step guide for AI assistants on how to conduct a research project.

> **MCP or REST API?** This guide shows MCP tool names and REST endpoints side by side. Use whichever matches your integration. For MCP-specific details (nullable fields, content formatting, common pitfalls), see the [MCP Client Guide](/llms/mcp-client-guide.md). For REST API details, see the [OpenAPI spec](/api/openapi.yaml) (or the same document as [JSON](/api/openapi.json)) — it is generated from the routes the server registers, so it covers every one of them and states which credential each needs. The endpoints named below are the ones each step maps to, not the whole surface.

## Overview

A research project follows this lifecycle:

1. **Initialize** — Design the research structure (sections, goals, tags)
2. **Conduct** — Interview the user, create entries, work the marks they left on
   the text, track progress
3. **Complete** — Mark sections and research as completed

### Check first that you may write

A research belongs to a team, not to whoever created it, and your role there can
be read-only. Before continuing someone else's research — a session, an entry, a
task, a status change — confirm you may write to it:

- `research_list` marks a shared research with `team` and a read-only one with
  `access: "read-only"`. No `access` key means you may write.
- `research_get` returns `role` on the research: `viewer`, `editor` or `owner`.

A `viewer` gets `your role in this team does not allow this` on the first write.
Discovering that after an interview wastes the user's answers, so check before
you ask. A research you created yourself is always writable. See
[Access](/llms/mcp-client-guide.md#access-you-can-see-more-than-you-can-write).

## Step 1: Initialize

Use the `research/initialize` MCP prompt, which runs this in three turns, or do
it call by call:

1. Find out what decision is waiting on the research before you propose any
   structure. A section list shown first anchors the person to it.
2. **Check whether a methodology already covers this.** `template_list` returns
   the kickoff methodologies available — matching criteria only, no bodies — and
   `template_get` returns the one that fits in full: what to ask before proposing
   anything, what structure to suggest, what a good entry looks like here, and
   when the research is finished. Follow it; it is instructions, not a form. An
   empty list, or nothing that fits, is a normal answer — design the research
   yourself and say so.
3. Define a clear, specific research goal
4. Design the sections *from that conversation*. Fewer than you think: create one
   when you have something to put in it, since the conductor aims at the
   least-covered sections and an empty one is a standing instruction to invent
   content for it. Each needs a slug name, display name, and description
5. Add relevant tags for categorization
6. Create it with `research_create`, passing `template_slug` when you followed a
   methodology. That stamps `template_slug` / `template_version` on the research
   and attaches the skills the methodology names — read `skills_attached` and
   `skills_unavailable` in the reply. The slug is an MCP argument only; `POST
   /api/researches` has no field for it
7. Record scope and success criteria in `description` and `goal`. For working
   rules specific to this research, use `skill_create` with `research_id` to
   create an attached private skill with a concrete trigger description. Keep
   reusable methodology in team or built-in skills.

See [Templates](/llms/templates.md).

## Step 2: Conduct Research Sessions

### Note the Skills, Load Them Late

`research_get` returns a `skills` array — each entry a name, a tier and one line
saying **when** to use it. The bodies are not there. The array is populated even
for a research nobody has curated, because the product skills are in every
index; a missing key means the built-ins failed to load.

Read the lines when you load the research; do not load the bodies then. When you
are about to do the work one of them names — start an interview, grade a source
that two people disagree about, build a roadmap — call `skill_load` with that
slug and read it at that moment. One slug per call. A skill read three steps
before the work it describes has usually been forgotten by the time it matters.

Where two skills conflict, the higher tier wins: research-private over team over
built-in, which is why the index arrives in that order. Research-specific tone
and depth requirements belong in private skills. See [Skills](/llms/skills.md).

When the index does not cover the work in front of you, you can change it.
`skill_list` shows what this research could attach and how many of its six slots
are spent; `skill_attach` takes one up, `skill_create` writes a new one. Do it
when a methodology is missing or wrong, not as a warm-up — a research whose
skills were curated by an agent that had not yet done any of the work is a
research following guesses. And say what you changed: which skills a research
follows is the user's decision to review.

Three refusals to expect. **Six chosen skills** is the whole budget, and a
seventh — attached or newly written, since writing a private one attaches it —
is `skill_cap_reached`; free a slot before you retry, and not by dropping a
product skill, which is outside the budget and refuses with `not_allowed`.
**`skill_detach` deletes a research-private skill** rather than shelving it: it
exists nowhere else, and the answer says `deleted: true`. And **a slug is fixed
when the skill is created** — `skill_update` renames it without ever changing the
slug, so a `slug_taken` is cleared by editing or deleting whatever holds the
name, never by picking a new one.

### Picking Up a Research That Is Already Running

Most sessions are not the first one. A new chat has no memory of the last, and
walking every section with `entry_list` to find out where the work stopped costs
more the larger the research is.

`research_resume(research_id, session_id, limit)` returns the queue instead:
tasks `in_progress`, `blocked` and `pending`; the selected session's open and
deferred questions; the marks a person left, split into `to_work` and
`awaiting_human`; the documents changed most recently; and at most three
`next_actions`, each with a `reason_code`, a sentence saying what it was derived
from, and an `actor`. Call it after `research_get` — that one carries the
constraints (`memory`, each section's `instruction` and `field_spec`, the skills
index), this one carries the work — and before you start reading documents.

Four things about the answer decide whether you use it correctly:

- **`actor: "human"` is not your work.** An answered mark is waiting for the
  person who raised the objection to accept it, and you cannot accept your own
  answer. So is `choose_session`. Report those; do not queue them.
- **An empty group is not a finished research.** Every group carries `returned`,
  `total`, `has_more` and a `more` object naming the tool that opens the rest
  (`task_list`, `session_get`, `annotation_list`, `entry_list`). A top-five is a
  top-five.
- **`author_kind: "human"` on a recent entry is a correction.** Somebody edited
  that document in the web UI after the last session. Read it — `entry_history`,
  `entry_diff` from the `revision` given — before you touch it. Extending it is
  the point; undoing it is the failure this list exists to prevent.
- **It never picks a session for you.** With one active session it selects it;
  with several it returns them all with `selection_required: true` and no
  `selected_id`, and the question groups come back empty rather than merging two
  interviews. Ask which one, then pass `session_id` (a UUID or an `SS` code in
  that research). With none active it shows the most recently created one with
  its real status — starting a new one is still a separate, deliberate write.

Two things it is not. It is **not a change log**: `recent_entries` is what was
touched most recently and its `total` is how many documents the research has,
not how many changed, while a deleted document leaves no trace at all. And it is
**not the personal new/changed queue** — reading it marks nothing as seen, and
that queue has no MCP tool by design.

Above roughly 24 KiB the response is shortened: previews are cut first, then
examples, and `truncated: true` with a one-line `note` says so. Totals,
`has_more` and the `more` links survive every step, so a shortened answer still
reports how much there really is.

### Create a Session

Start a Q&A session focused on specific sections:

1. Create a session with `session_create` (MCP) or `POST /api/sessions` (API)
2. Include initial questions covering gaps in the research
3. Set a focus area to keep the interview directed

### Interview Loop

For each question:

1. Present the question clearly to the user
2. Record their answer with `question_update`
3. If the answer raises follow-ups, add them with `question_create`
4. Defer or skip questions that are out of scope

### Create Entries

As information accumulates:

1. Write entries with well-structured markdown in `entry_create`
2. Place each entry in the appropriate section
3. Use tags for cross-cutting concerns
4. Title and description are auto-generated from content if not provided
5. Use `[[E1]]` syntax to cross-reference other entries
6. Each entry gets an auto-assigned short code (E1, E2, ...)
7. Entries created while a session is active are linked to it automatically, which is what the session export lists as "entries produced in this session"

**Read the section's instruction before you write.** `section_list` (or
`research_get`) returns `instruction` on a section that has one: three to six
imperatives saying what a document *here* looks like — "Name the producing
service. State the consumer. One paragraph of rationale, then the payload."
Follow it for this document. It is the most specific of the three places a rule
lives — the research's memory says what this research is, a skill says how a kind
of work is done, this says how to write in this section — so it wins on a direct
conflict about the shape of the document, and only about that. Where the section
declares fields too, the instruction names those keys. Nothing checks that you
read it; the document you write is the only evidence. Write one with
`section_update` when a section has grown a convention its documents keep
repeating in their own words. See [Skills](/llms/skills.md).

**Check whether the section declares fields.** `section_list` (or `research_get`)
returns `field_spec` on a section that holds one class of document — a
specification, a vendor, a decision record. Pass those keys in `metadata` on
`entry_create`; the vocabulary is closed, so any other key is dropped and named
in `metadata_report`, and a section that declares nothing accepts none. Where you
do not know a value, send `null` rather than a plausible guess: `null` is an
explicit unknown and it answers a required field. Read the section's existing
documents first — the first few entries set the pattern for every one after. See
[Document Metadata](/llms/metadata.md).

**If the user has the document already, do not ask them for a file.** Paste it
into `entry_create` — you can set `title`, `description`, `status`, `tags` and
`metadata` in the same call. Dropping a `.md` file into a section is a separate,
human path (the **Import .md** button on the section view), it has no MCP tool,
and it guesses at the fields you would otherwise state outright. Point a user at
it only when the file is on their disk and not in the conversation. See
[Export → One File into a Section](/llms/export.md).

### Build on What Exists, Don't Overwrite It

Before rewriting an entry a previous session produced:

1. `entry_history` — who wrote it last, in which session, and what they changed
2. `entry_diff` — the change itself, if the history suggests someone corrected something

A revision whose author is `human` is a person's edit in the web UI: treat it as
the strongest reason to extend rather than replace what is there.

Every write that changes something leaves a revision, so nothing is permanently
lost and an earlier version can be restored. But undoing another session's
correction without noticing is exactly the failure this history exists to catch.
See [Revisions](/llms/revisions.md).

### Work the Marks They Left

A person reading what you wrote can mark a sentence they do not believe. Those
marks are a queue, and they are the cheapest high-quality signal in the system:
a human read this specific text and said what is wrong with it. Nothing pushes
them at you, so make the check part of the cycle.

**When to look:** at the start of a session on a research that already has
documents, and again after a pass of entry writing. Not on a research you just
created — nobody has read it yet.

1. `annotation_list(research_id, status: "open")` — the queue, capped at 15 by
   the server. It carries the quote, the block's current text and the kind of
   work asked for, so you can triage without reading a single document.
2. **Say what you will take, before you take it.** Group by document — the
   response comes back ordered by entry for exactly that reason — and let the
   user cut the list. A pass is scoped and accepted as a unit.
3. Work each mark by its `kind`: `verify` (find a source or say you could not),
   `dig` (write a child entry and link it from the marked block with `[[E19]]`),
   `disagree` (**record both positions — do not edit the objection away**).
4. Write **one revision per document, not one per mark**: a pass the user rejects
   is then undone with a single restore.
5. `annotation_answer(annotation_id, resolution)` on each, naming what you
   produced — `[[E19]]`, `[[Q7]]` — which become real links.
6. Stop at `answered`. Closing and dismissing are the user's, always. Tell them
   the pass is ready for review.

When you cannot answer without them, do not invent one: `question_create` in the
active session, then answer naming it — *"needs you — asked [[Q9]]"*. A mark whose
`attempts` is already 2 is one nobody can settle from inside the system; escalate
it instead of writing a third answer.

See [Annotations](/llms/annotations.md).

### Track Progress

- Use `research_update` with `add_memory` and the actual research `session_id` to record key insights; use `research_memory` for individual edits/deletes
- Update session notes with `session_update` using `add_note`
- Use tasks (`task_create`) to track work items
- When a document *is* the plan, show those tasks in it with a `task_ref` block
  (`entry_type: blocks`) instead of retyping them as a checklist — it references
  the tasks by their `task_id`, so the document and the board cannot drift apart.
  See [Block Documents](/llms/blocks.md)
- Mark sections as completed when they have sufficient coverage

### Build Roadmaps

When the research has a natural progression, sequence, or decision tree, create a visual roadmap:

1. Identify whether the topic suits a roadmap (learning path, strategy, migration plan, onboarding flow)
2. Create a roadmap with `roadmap_create` (MCP) or `POST /api/roadmaps` (API)
3. Include all nodes and edges in one call using `temp_id` for node references in edges
4. Define custom `statuses` that fit the domain (e.g. `["not_started", "learning", "mastered"]`)
5. Update node statuses with `roadmap_update_node` as the user progresses
6. Extend the graph with `roadmap_add_nodes` as new steps emerge

**When to create a roadmap during research:**

| Situation | Action |
|-----------|--------|
| Topic has a clear learning sequence | Create a roadmap after initial Q&A reveals the learning path |
| Research uncovers a multi-step process | Build a roadmap showing the steps and dependencies |
| User asks "how do I get from A to B?" | Create a roadmap with the progression |
| Multiple alternatives exist | Use decision nodes to show branching paths |
| Research maps a system architecture | Create a roadmap showing component dependencies |

**Example: After a session on "learning Vue 3", create a roadmap:**
- Nodes: HTML/CSS basics → JavaScript ES6+ → Vue 3 Fundamentals → Composition API → State Management → Testing → Deployment
- Edge types: `default` for the main path, `optional` for alternatives
- Statuses: `not_started`, `in_progress`, `completed`

**Tips:**
- Create roadmaps when enough information has been gathered (typically after 1-2 sessions)
- Use `milestone` nodes to mark key achievements or checkpoints
- Use `decision` nodes when the path branches based on choices
- Use `info` nodes for prerequisites or reference material that isn't a step
- Keep node descriptions concise — detailed content belongs in entries, link conceptually

## Step 3: Complete

1. Mark entries as completed with `entry_update`. This is the one write that
   required metadata can refuse — `cannot complete: required metadata is
   unanswered: …`. Fill those fields, or send `null` for the ones nobody can
   honestly answer; reach for `allow_incomplete: true` only as a decision you can
   defend
2. Mark all sections as completed with `section_update`
3. Mark the research as completed with `research_update`
4. The web UI shows the full research with all entries, questions, and tasks
5. Hand the user a document if they want one — see [Export](/llms/export.md):
   - the research and per-session export pages produce markdown or PDF
   - `research_export` with `format: "obsidian"` returns a link to a zip shaped like an Obsidian vault (a folder per section, a note per entry, `[[E3]]` resolving as a link) — offer this when the user keeps notes in Obsidian or wants the research as files. The link needs their bearer token
   - `research_export` with no `format` returns the portable JSON, which is for moving the research to another server, not for reading
6. **Finishing is not deleting.** `research_update` with `status: "completed"`
   records that the work is done, and `archived` puts the research out of the
   way; both are reversible and one of them is what "we're done with this"
   means. `research_delete` destroys the research and everything in it with no
   trash and no restore, needs `confirm: true` and an owner, and is only ever
   right when a person named that research and asked for it to be gone — see
   [MCP Client Guide → Deleting Is Permanent](/llms/mcp-client-guide.md#deleting-is-permanent)

## Short Codes

Every record gets an auto-assigned short code on creation:

| Entity | Prefix | Scope | Example |
|--------|--------|-------|---------|
| Research | `R` | global | `R1`, `R2` |
| Section | `S` | per research | `S1`, `S2` |
| Entry | `E` | per research | `E1`, `E2` |
| Session | `SS` | per research | `SS1`, `SS2` |
| Question | `Q` | per session | `Q1`, `Q2` |
| Task | `T` | per research | `T1`, `T2` |
| Roadmap | `RM` | per research | `RM1`, `RM2` |
| Node | `N` | per roadmap | `N1`, `N2` |
| Annotation | `A` | per research | `A1`, `A2` |

REST responses carry the `code` field on every entity. MCP tools are less complete: `research_create`, `research_import`, `research_get`, `entry_create`, `entry_list`, `entry_read`, `entry_patch`, `session_get`, the two `annotation_*` tools and the `roadmap_*` tools return codes, while section, question and task codes are only reachable through the REST API. Codes can be used in URLs instead of UUIDs (`/research/R1/entry/E2`), but only some tools resolve them as arguments — the [MCP Client Guide](/llms/mcp-client-guide.md) carries the list, and everything else wants the UUID.

## Cross-References

Use `[[...]]` syntax in entry content to create links between documents:

- `[[E3]]` — link to entry E3 in the same research
- `[[R2:E5]]` — link to entry E5 in research R2
- `[[R2]]` — link to research R2
- `[[RM1]]` — link to roadmap RM1 in the same research
- `[[RM1:N3]]` — link to node N3 in roadmap RM1

### How it works

1. When an entry is created or updated, the server parses all `[[...]]` patterns from the content
2. Each reference is resolved to a target entry/research UUID and stored in the `crossrefs` table
3. If the target doesn't exist yet (e.g. `[[E5]]` before E5 is created), the reference is stored as unresolved
4. Use `POST /api/researches/{id}/crossrefs/rebuild` to re-scan all entries and resolve stale references
5. On server startup, codes are automatically backfilled for any records missing them
6. If the target is **deleted**, the reference is not: the stored row is kept and marked unresolved, and the `[[R1:E5]]` stays in the citing document exactly as it was written. Nothing edits somebody else's text to hide that what it cited once existed, and a rebuild cannot repair it — there is nothing left to resolve to

### Viewing cross-references

- **In entry view**: `[[E3]]` renders as a clickable badge-style link navigating to the target entry
- **In mindmap**: cross-references appear as purple dashed edges between entry nodes. Hover an edge to see which entries are connected and highlight the source/target nodes
- **Via API**: `GET /api/researches/{id}/crossrefs` returns all resolved and unresolved references

### Best practices for cross-referencing

- Reference foundational entries from higher-level ones: "See [[E1]] for goroutine basics"
- Use cross-research references when topics span projects: "Compare with [[R2:E3]]"
- After creating entries that are referenced by earlier entries, run rebuild to resolve forward references
- Keep entries self-contained — cross-references add context but each entry should be readable alone

## Best Practices

- Ask one question at a time for clarity
- Prioritize high-priority questions first
- Write entries that are self-contained and useful on their own
- Load relevant private skills for the research's tone and depth requirements
- Load a skill when you reach the work it names, not while orienting — and one at a time
- Read the open annotations before writing into a research somebody has already read — a marked sentence is a person telling you where the document is wrong
- Answer a mark, never close it: `closed` and `dismissed` belong to the reader
- Keep session notes updated for context across sessions
- Use tasks to plan and track remaining work
- Use `[[E1]]` cross-references to build connections between related entries
- Run crossref rebuild after batch-creating entries to resolve forward references
- Create roadmaps when the research reveals step-by-step processes, learning paths, or decision trees
- Build the full roadmap graph in one `roadmap_create` call rather than adding nodes one at a time
- Choose roadmap statuses that match the domain vocabulary (learning, marketing, engineering, etc.)
