# Export

How to export research data as documents. Three scopes exist: a whole research, a single session, and a single document. The first two come in a JSON form (structured data + pre-rendered markdown) and a raw `.md` download; a document leaves only as a `.md` file, with YAML front matter. A whole research has two further forms: an **Obsidian vault** (`?format=obsidian`, a zip of linked notes — a document to read) and the **portable JSON** (a research to move to another server). Everything here is a read endpoint except the two imports, the only writes on this page: the **portable JSON**, which re-creates a whole research on another server, and a **markdown file dropped into one section** (`POST /api/sections/{id}/import`), which is the other half of the single-document download.

## Export Pages (Web UI)

| Scope | Page | Reached from |
|-------|------|--------------|
| Research | `/research/{code}/export` | Export button on the research page |
| Session | `/research/{code}/session/{sessionCode}/export` | Export button on the session page |

Both pages render a single printable document and expose the same two actions:

- **Download .md** — saves the markdown built by the server.
- **Print / PDF** — opens the browser print dialog (Ctrl+P / Cmd+P). Choose "Save as PDF". Navigation, footer, and interactive elements are hidden in print.

The research page renders: research header (title, goal, description, tags), table of contents with anchor links, all sections with full entry content, all sessions with questions and answers, all tasks with statuses and results.

The session page renders: parent research name, session title, focus, code, status, notes, all questions with answers, and the entries produced during that session.

### Print Individual Entries

Open any entry page and use Ctrl+P / Cmd+P. The entry page has print-optimized CSS that hides navigation, action buttons, cross-references, and related entries, showing only the breadcrumb path and document content.

The same page's `⋯` menu offers **Download .md** — the document as a file with
YAML front matter, described in [One Document as a File](#one-document-as-a-file).

The reverse lives on the section view of a research page: an **Import .md**
button beside the view toggle, and a drag-and-drop target over the entry list.
Both are absent for a `viewer`. See [One File into a Section](#one-file-into-a-section).

## Research Export API

```
GET /api/researches/{id}/export
```

`{id}` accepts a UUID or a research short code (`/api/researches/R1/export`).

Returns all research data as structured JSON:

```json
{
  "research": { "name": "...", "goal": "...", ... },
  "sections": [
    {
      "name": "...",
      "display_name": "...",
      "entries": [
        { "title": "...", "entry_type": "markdown", "content": "full markdown...", "tags": [...] }
      ]
    }
  ],
  "sessions": [
    {
      "title": "...",
      "questions": [
        { "text": "...", "answer": "...", "status": "answered" }
      ]
    }
  ],
  "tasks": [
    { "title": "...", "status": "completed", "result": "..." }
  ],
  "roadmap_count": 2,
  "markdown": "# Full document as markdown string..."
}
```

`roadmap_count` is a number, not the roadmaps: this payload is already the whole
research, and the count exists so a client can tell whether the roadmap option of
the vault export applies at all. Use `roadmap_list` / `GET /api/researches/{id}/roadmaps`
to read them.

### Download Markdown File

```
GET /api/researches/{id}/export?format=md
```

Returns `text/markdown` with `Content-Disposition: attachment`, filename derived from the research name (spaces become `_`, `/ \ : " '` are stripped or replaced).

The markdown document structure:
1. Title and research header (goal, description, tags)
2. Table of contents
3. Sections with full entry content
4. Sessions with all questions and answers
5. Tasks with statuses and results

**Document metadata** — the values for the fields a section declares — is
rendered as a labelled block above each entry's body, in the order the section
declares its fields. A declared field nobody answered prints an em dash rather
than being skipped: a reader of the file has no other way to learn the field
exists and is empty, and that gap is what "incomplete" looks like on paper.
Values under keys the section has since dropped are kept in the database but not
printed. A section that declares nothing prints no block at all, and the session
export below renders no metadata in any case. See
[Document Metadata](/llms/metadata.md).

## Session Export API

```
GET /api/researches/{id}/sessions/{sessionId}/export
```

Exports one session instead of the whole research. Both path segments accept a UUID or a short code, so `/api/researches/R1/sessions/SS1/export` works.

Returns JSON:

```json
{
  "research": { "name": "...", "code": "R1", ... },
  "session": { "title": "...", "code": "SS1", "focus": "...", "status": "active", "notes": "..." },
  "questions": [
    { "text": "...", "area": "...", "rationale": "...", "priority": "high", "status": "answered", "answer": "..." }
  ],
  "entries": [
    { "title": "...", "code": "E4", "section_id": "...", "entry_type": "markdown", "content": "full markdown...", "tags": [...] }
  ],
  "section_names": { "<section-id>": "Display name of the section" },
  "markdown": "# Session as markdown string..."
}
```

- `questions` — every question of the session, unfiltered, ordered by `position`.
- `entries` — only entries whose `session_id` equals this session, with full content. `entry_create` links the entry to the research's active session automatically when `session_id` is not given, so entries created during an interview show up here without extra work. An entry the session **edited** without creating is not here; `GET /api/sessions/{id}/changes` covers those, with the revision range and a diff — see [Revisions](/llms/revisions.md).
- `section_names` — map of section ID to display name (falls back to the slug `name`), so the client can label each entry without a second request.

### Download Markdown File

```
GET /api/researches/{id}/sessions/{sessionId}/export?format=md
```

Returns `text/markdown` with `Content-Disposition: attachment`, filename derived from the session title.

The markdown document structure:
1. Session title, parent research name, code, status
2. Focus as a blockquote, then `## Notes` if the session has notes
3. `## Questions (N answered of M)` — one `### Q:` block per question with area, priority, status, rationale, and answer
4. `## Entries produced in this session` — each entry with its code, section, tags, status, and full content (omitted when the session produced no entries)

## Obsidian Vault Export

```
GET /api/researches/{id}/export?format=obsidian
```

The same route as the JSON export, behind `format=obsidian`. Returns
`application/zip`: the research as a folder of cross-linked markdown notes, ready
to unzip into an [Obsidian](https://obsidian.md) vault. Any editor that reads
markdown can open it; only the links are Obsidian-flavoured. The archive is built
whole before the first byte is sent, so a failure is still a status code rather
than a truncated zip.

The archive has no wrapper folder — its entries are at the root, because every
extractor already creates a folder named after the zip. On the command line use
`unzip -d "R1 — Competitive Landscape" file.zip`, or the files land in the
current directory.

```
R1 — Competitive Landscape.zip
├── README.md                     goal, description, memory, private skills, and a
│                                 linked table of contents of every entry,
│                                 session, task and roadmap in the vault
├── 01 — Market Analysis/         one folder per section, numbered in section order
│   └── E1 — Pricing model.md
├── 02 — Competitors/
├── Unfiled/                      entries whose section is gone (usually absent)
├── Sessions/SS1 — Initial exploration.md
├── Tasks/T1 — Verify the claim.md
├── Roadmaps/RM1 — Migration plan.md      nodes and edges as a mermaid diagram
├── _html/E2 — Revenue chart (13b3d97a).html
├── _history/E1.md                only with revisions=true
└── _Unresolved/R2 E5.md          one note per reference pointing outside the export
```

**A cross-reference is retargeted at the note's filename and keeps its code as
the display text**: `[[E3]]` is exported as `[[E3 — Pricing model|E3]]`. The
reader sees exactly what the author wrote, and the link resolves — so backlinks,
unlinked mentions and the graph view work.

Obsidian's `aliases` do **not** resolve a hand-written link. An alias makes a
note findable in the quick switcher, in search and in link autocomplete, where
choosing a suggestion inserts a link to the *filename* with the alias as display
text; a `[[E3]]` typed into a file is matched against filenames only, and left
alone it renders as an unresolved link that offers to create an empty note.
Aliases are still written, for the switcher.

The retargeting happens on the way out and never in storage — the same kind of
rendering as a block document becoming markdown. Re-exporting rebuilds every link
from the current titles, so renaming an entry cannot leave a stale link behind.

- **Entries, sessions, tasks and roadmaps each get a note**, because each is a
  link target. A node link (`[[RM1:N3]]`) lands on its roadmap note, and
  `[[R1:E1]]` — the qualified form of a reference inside the same research —
  lands on the same note as `[[E1]]`.
- **Every note links home**, with a relative markdown link to `README.md` rather
  than a wikilink: two exports unzipped into one vault hold two `README.md`
  files, and a name could not tell them apart.
- **A reference to something outside the export** — another research, a deleted
  entry, a folder the options omitted (`[[SS1]]` with `sessions=false`, `[[T1]]`
  with `tasks=false`) — gets a stub under `_Unresolved/` explaining what it points
  at. The link is a real reference, so it stays visible as one rather than
  dangling silently. Only text shaped like a code (`E12`, `R2:E5`, `RM1:N3`)
  qualifies, and at most 200 stubs are written.
- **An entry's frontmatter** carries `code`, `title`, `aliases`, `research`,
  `section`, `type` (`markdown` or `blocks`), `status`, `tags`, `created`,
  `updated`, and the `session` that produced it. With `revisions=true` it also
  carries the current `revision` and its `author` (`agent`, `human`, `import`,
  `restore`); without that option neither key is present, whatever history the
  entry has. Empty values are dropped rather than written blank. A session note
  carries `code`, `title`, `aliases`, `research`, `focus`, `status`, `created`,
  `updated`; a task also `priority` and `completed`; a roadmap also `statuses`.
  Frontmatter is written with a YAML encoder, so a title containing `:` or a
  quote still parses.
- **A section's declared fields follow the eleven system keys**, in declared
  order, so two notes from the same section scan the same way. Those eleven keys
  are refused as field keys precisely so nothing here can overwrite them. Three
  rules differ from the system keys above and none of them is cosmetic: a
  declared field with **no value is written as an explicit `null`** rather than
  dropped, because a vault query for "documents missing this field" would
  otherwise find nothing and the documents worth finding would be exactly the
  invisible ones; a field answered with an explicit unknown emits the word
  `unknown`, because somebody did look; a `ref` value is emitted in its
  `"[[E47]]"` bracket form so Obsidian treats the property as a link; a repeated field is a real YAML
  sequence even with one element, so a membership filter matches. Numbers stay
  unquoted and sort numerically. See [Document Metadata](/llms/metadata.md).
- **Filenames** are `E1 — Title` and keep Cyrillic and other non-ASCII. What a
  filesystem refuses is removed (`: * ? " < > |`), and so is what would cut a
  wikilink short (`# ^ [ ]`); a path separator becomes `-`,
  newlines and control characters become spaces, Windows reserved names get a
  trailing `_`, and a component longer than 100 bytes is cut on a rune boundary.
  The code leads the name, so two entries with the same title cannot collide.
- **Block documents** render through the same markdown projection as the `.md`
  export, so checklists keep their ticks and mermaid stays a fence — which
  Obsidian draws. The live-editor link under a diagram is omitted here for that
  reason.
- **An `html` block becomes a real file** under `_html/` with a callout linking
  to it. Obsidian sanitizes inline HTML and runs no scripts, so an inlined
  artifact would be a broken shell of itself; a separate file opens in a browser
  and is still the artifact.
- **A tag Obsidian cannot index** (one containing a space, a tab or `#`) stays
  exact in the frontmatter, and a line at the end of the note names the tags that
  will not appear in the tag pane. The vocabulary is the author's; it is not
  mangled to fit a reader.

Query options — everything is included by default except revisions:

| Param | Default | Effect |
|---|---|---|
| `sessions` | `true` | `false` omits `Sessions/` and the session link in each entry's footer. The `session` frontmatter key stays: it is provenance, not a link |
| `tasks` | `true` | `false` omits `Tasks/` |
| `roadmaps` | `true` | `false` omits `Roadmaps/` |
| `html` | `true` | `false` omits `_html/`; the callout stays and says the file was left out |
| `revisions` | `false` | `true` adds `_history/{code}.md` for every entry that has history — a table of revision, date, author, session and summary — plus a link to it in the entry's footer, and the `revision` / `author` frontmatter keys |

`false`, `0`, `no`, `off` and `n` turn an option off; `true`, `1`, `yes`, `on`
and `y` turn it on (case-insensitive). Anything else keeps the default rather
than guessing, and a mistyped flag never turns the export into an error.

### From the MCP tool

`research_export` takes an optional `format`:

| Value | Result |
|---|---|
| absent, `null`, `""`, `portable`, `json` | the portable JSON payload below — unchanged, and the input for `research_import` |
| `obsidian`, `vault`, `zip` | a JSON object describing the vault download |
| anything else | a validation error: `format must be portable or obsidian` |

A tool result cannot carry a zip, so `format: "obsidian"` returns the link
instead:

```json
{
  "format": "obsidian",
  "url": "https://host/api/researches/R1/export?format=obsidian",
  "research": "Competitive Landscape",
  "contains": "README.md with a linked table of contents, a folder per section, ...",
  "links": "Cross-references are retargeted at the notes they name and keep their codes as display text ...",
  "auth": "The link needs the same credentials as the REST API ...",
  "description": "Download the zip and unzip it into a folder in an Obsidian vault. Options: ..."
}
```

- The research is resolved before the link is built, so a bad `research_id` is an
  error now rather than a link that 404s later.
- `url` is absolute when the server has a base URL configured; otherwise it is the
  bare path `/api/researches/R1/export?format=obsidian` and `description` says so.
  Do not invent an origin for it.
- The URL is a normal read endpoint: with `auth_enabled` it needs a bearer
  credential — a JWT, an API key or an OAuth access token — as an
  `Authorization: Bearer` header. The instance `api_token` is not one of them; it
  is the operator's credential and no read route accepts it in a user's place.
  Pasting the URL into a browser tab only works on a server running without auth.
  Say that when you hand the link to a user.
- The query options above apply to that URL; the tool has no parameters for them.

**Annotations travel in the portable dump and nowhere else.** They are working
process — one person saying they do not believe a sentence — so no reading
export carries them, and no share link exposes them. A move is different: the
dump keeps each mark's quote, block id, kind, note, status, answer and the
reasons any answer was refused. Ids, short codes, the author's account and the
anchored revision do not travel, and the anchor is resolved against the document
as it arrives — a mark whose sentence survived the move is anchored, one whose
sentence did not is orphaned, exactly as after any other rewrite.

**The vault does not travel back.** It is a document format; use the portable
JSON below to move a research between servers.

## One Document as a File

```
GET /api/entries/{id}/markdown
```

Every other export here answers "give me everything". This answers "give me this
one", which until now had no answer at all: a document could leave inside a
research or a session export and in no other way.

`{id}` is the entry **UUID**. This route resolves no `E`-code and accepts no
`?research=`, unlike `GET /api/entries/{id}`, which does both. It is a read
endpoint on the usual terms — unauthenticated by default, bearer token when
`auth_enabled` is set, and a `viewer` may download exactly as an `editor` can.

The response is `text/markdown; charset=utf-8` with `Content-Disposition:
attachment` carrying the filename twice: an ASCII `filename=` in which every
non-ASCII rune became `_`, and the RFC 5987 `filename*=UTF-8''…`. A client that
reads the second gets `E50 — SPEC-01 · Payload состояния площадки.md`; one that
reads only the first still gets a distinguishable name instead of a mangled one.

The name is the code, an em dash, and the title, sanitised by the vault's own
function — so a downloaded file and the same document's note in a vault are named
alike. Non-ASCII stays; `: * ? " < > |` and `# ^ [ ]` are dropped, a path
separator becomes `-`, newlines and control characters become spaces, a Windows
reserved name gets a trailing `_`, and each part is cut to 100 bytes on a rune
boundary. The code leads because a folder of downloads then sorts into the order
the research issued them, and two documents with the same title cannot collide.

### Front matter

Built by the code that builds the vault's, deliberately: two copies of these
rules drift, and the drift is invisible until somebody diffs two exports of the
same document.

```yaml
---
code: E50
title: SPEC-01 · Payload состояния площадки
description: What the scanner sends when a site changes hands.
research: R21
section: Specifications
type: markdown
status: active
tags: [spec, contract]
created: "2026-08-18T09:12:44Z"
updated: "2026-08-19T07:03:10Z"
stage: in-review
produces: [scanner-watchdog]
owner: null
---
```

Nine system keys — `code`, `title`, `research`, `section`, `type`, `status`,
`tags`, `created`, `updated` — plus `description` when a person wrote one, then
the fields the section declares, in declaration order. Two of the vault's eleven are absent on purpose: `aliases`,
which exists so Obsidian's quick switcher finds a sibling note, and `session`,
which is provenance. Timestamps are RFC 3339 in UTC.

An empty **system** value is dropped rather than written blank. A **declared**
field is always written: unanswered as `null`, because a query for "documents
missing this field" would otherwise find nothing and the documents worth finding
would be exactly the invisible ones; answered with an explicit unknown as the
word `unknown`, because somebody did look and this document is not the one you
are looking for. A `ref` value comes out in its `"[[E47]]"` bracket form, a
repeated field as a real YAML sequence even with one element, a number unquoted.
The field types and the eleven reserved keys are in
[Document Metadata](/llms/metadata.md).

`research: R21` is the entire identity guarantee. `code: E50` means nothing
outside the research that issued it; the file says where it came from and claims
nothing more.

Below the front matter comes the body and nothing else — no title heading is
prepended, no footer, no link home. A loose file has nowhere to link home to.

### What it deliberately does not do

- **It does not rewrite `[[E3]]`.** The vault retargets a reference at the note
  that answers it (`[[E3 — Pricing model|E3]]`) because Obsidian matches a
  wikilink against filenames. A loose file has no siblings, so the same rewrite
  would point at a note that exists nowhere; the reference is emitted exactly as
  stored, and in a foreign vault it resolves to nothing. That is accepted rather
  than overlooked: the file is offered at the user's own risk, and this product
  does not undertake to make the round trip lossless.
- **It carries no provenance.** No `session`, no `revision`, no author. Who wrote
  a document, during which interview and at which revision, is working process —
  the one thing this product never lets out, and the same reason a share link is
  refused it.
- **It does not repeat a derived description.** An entry's description is
  derived from lines 2-5 of its own content when nobody supplies one, and
  printing that above the body is the same sentences twice. A description
  somebody wrote by hand is the other case — it appears nowhere in the body, and
  omitting it meant the download dropped it silently and a re-import invented a
  different one from the prose. So `description` is emitted **only when the
  stored value differs from what the derivation would produce**. The vault
  writes none at all, which is why a note taken out of a vault loses a
  hand-written one; it does print a task's and a roadmap's, which are always
  written by hand.
- **A block document renders as its markdown projection**, the same one the
  `.md` export uses, with none of the vault's overrides: a checklist keeps its
  ticks, a `task_ref` is resolved against the research's tasks and printed with
  their titles and statuses, and a `mermaid` block stays a fence with the mermaid.live link under it
  (the vault is the one target that drops that link, because Obsidian draws the
  fence itself). An `html` block is **named, not emitted** — `> **Revenue chart**
  — interactive HTML, view in the web UI.` Writing the artifact beside the note
  and linking it is the vault's move, and a single file has no beside. A document
  that cannot be parsed yields the same one-line notice every other export uses.
- **There is no MCP tool.** An agent that wants the content calls `entry_read`;
  putting a file on a disk is a human act, and nothing about the tool list
  changes because this route exists.
- **It is not on the share sub-mux.** A visitor holding a share link cannot take
  a document away as a file, whatever the link's include flags say — see below.

## One File into a Section

The other half of the download above. One markdown file becomes one entry, in
two calls:

```
POST /api/sections/{id}/import/preview    multipart, the file in the `file` part   -> a parse, writes nothing
POST /api/sections/{id}/import            JSON fields                              -> one entry, 201
```

`{id}` is the section **UUID**; no `S`-code is resolved here. Both calls are
**writes** for permission purposes — `editor` or `owner` in the owning team. A
viewer is refused the preview as well as the commit: telling somebody what their
file would have become is offering them a control they do not have. A non-member
gets `404`, a member without the right `403`. **Neither route is on the share
sub-mux**, and the service refuses a share context on top of that, because this
is the only write in the product addressed by a bare section id.

### Two calls, on purpose

The preview writes nothing and returns what it made of the file. The commit takes
**fields, not the file** — so an edit somebody makes in the preview survives, and
so the commit has no parameter through which a refused key could come back. The
commit body is exactly `title`, `description`, `status`, `tags`, `body`,
`metadata`; there is no `session`, no `author`, no `code`, no timestamps, and
nowhere to put them.

### What the preview returns

```json
{"data": {
  "filename": "E50 — Pricing model.md",
  "title": "Pricing model",
  "title_source": "frontmatter",
  "description": "How we price, in one paragraph.",
  "status": "active",
  "tags": ["pricing", "модель"],
  "body": "…the document with its front matter removed…",
  "body_lines": 84,
  "metadata": { "stage": "review" },
  "metadata_report": { "spec_version": 3, "stored": ["stage"], "unknown_keys": [], "invalid_values": [], "missing_required": [] },
  "fields":  [{ "key": "status", "value": "shipped", "applied": false, "reason": "not one of draft, active, completed or archived — importing as a draft" }],
  "ignored": [{ "key": "code", "value": "E50", "reason": "a code is assigned by the research…" }],
  "refused": [{ "key": "session", "value": "SS2", "reason": "which session produced a document is recorded here, never claimed by a file" }],
  "unresolved_refs": [{ "ref": "E9999", "count": 3 }],
  "warnings": ["The file says it came from R99; you are importing it into R21. …"]
}}
```

- `title_source` is `frontmatter`, `heading` or `filename`, in that order of
  preference: the front matter `title`, else the document's first ATX heading at
  **any** level, else the filename with its extension and any leading `E50 — `
  stripped back off — which is literally "everything before the first ` — `", so
  a file named `Notes — draft.md` with neither a front matter title nor a heading
  imports as `draft`. A title from the filename is reported in `fields` even
  though nothing went wrong — it is the one the person will want to change.
- `fields` covers the four keys that become the entry itself (`title`,
  `description`, `status`, `tags`). It is a separate channel because a value the
  parser read and could not use is neither a metadata problem nor a key declined
  by policy: a misspelled `status` is silently replaced by `draft`, and that is
  exactly the loss the preview exists to surface. `applied` says whether the
  value in the payload came from the file.
- `metadata_report` is the ordinary one, from the same validator every other
  write uses — with one addition: its `unknown_keys` and `invalid_values` entries
  carry a `value`, filled by the import and by nothing else. An agent that sent a
  bad value still has it; a person holding a file they did not write does not.
  Every reported `value`, here and in `fields` / `ignored` / `refused`, is one
  line clamped to 120 runes, so a key holding a paragraph cannot push its reason
  off the screen. The values that become the entry are **not** clamped.
- `unresolved_refs` are the `[[…]]` codes that name nothing in **this** research,
  with how often each appears. They are listed and left exactly as written.
- `warnings` are true but not actionable by us — the file naming a different
  research or section than the one it was dropped on. Never a refusal: moving a
  document between researches on purpose is the ordinary case.

### The three closed key sets

Every front matter key falls into exactly one of four buckets. The three closed
sets are matched **case-insensitively**; a metadata key is matched **exactly**
against the section's declaration, so `Stage:` is an unknown key where `stage:`
is a value.

| Bucket | Keys | What happens |
|---|---|---|
| **Accepted** | `title`, `description`, `status`, `tags` | become the entry; remarks land in `fields` |
| **Ignored** | `code`, `created`, `updated`, `research`, `section`, `type`, `aliases` | read, reported in `ignored`, dropped — each is a fact the system owns, and a file asserting it is stale at best and a collision at worst |
| **Refused** | `session`, `author`, `author_kind`, `revision`, `revisions`, `history` | read, reported in `refused`, never used — provenance is this product's own assertion and the whole history model rests on that |
| anything else | — | a candidate metadata value, validated against the section's `field_spec` |

The distinction between ignored and refused is not fidelity, it is trust. A
degraded file is the user's problem; a file that could **corrupt our records** is
ours. Accepting `session:` or an author out of a text file would let anybody
fabricate provenance.

`type` is ignored, so **a dropped file always lands as a `markdown` entry**. A
block document is written, not uploaded — see [Block Documents](/llms/blocks.md).

### Caps and refusals

| Cap | Value |
|---|---|
| file size | 1 MiB (`1048576` bytes) |
| extensions | `.md`, `.markdown` |

Both are served on `GET /api/metadata/schema` as `import_max_bytes` and
`import_extensions`, so a client never hard-codes a number that changes on the
server.

**Import is the one write in this product that refuses rather than reports.**
Everywhere else the author is a model in the middle of an interview and a
rejection destroys answers a person has already given; here a person is standing
over the file with an undo. Each refusal is `400`:

| Message stem | When |
|---|---|
| `only a .md or .markdown file can be imported` | the extension is anything else |
| `that file is larger than 1024 KB` | over the cap, whether the service sees the bytes or the request reader stops the upload first — checked before the extension, so an oversized `.pdf` is refused for its size |
| `that file has no content once its front matter is removed` | nothing but front matter |
| `the front matter could not be read: …` | malformed YAML. The parser's own message is carried through, **renumbered** so the line it names is the line in the file the person is looking at — the opening `---` is line 1. An unterminated block reads `it opens with --- but never closes the block` |
| `this document cannot be imported as given: …` | commit only: no title, no body, an invalid status, or metadata the preview had already shown to be wrong |

That last one is the only strict metadata path in the product: the commit refuses
on any `unknown_keys` or `invalid_values`, because the preview already showed the
person exactly that. `missing_required` is not a refusal — an imported document
may be incomplete like any other. See [Document Metadata](/llms/metadata.md).

The upload itself has three more `400`s, before any of the above: *expected a
multipart upload with one file in the `file` field*, *no file in the `file`
field*, and *that file could not be read*. The commit answers *invalid JSON body*
to a body it cannot decode.

### How the file is read

- A file with **no front matter at all is the ordinary case**, not an error —
  most markdown in the world has none. A leading BOM is stripped, CRLF becomes
  LF, and the closing fence is a line of exactly `---` or `...`.
- The body is stored **verbatim**. Every other write expands a literal `\n`,
  because MCP clients really do send escaped newlines inside JSON strings; a file
  has no such problem, and expanding them would rewrite the code block the
  document was written to explain.
- `tags` accepts a YAML sequence, and also the `tags: a, b` line people write by
  hand — split on commas, with the guess admitted to in `fields`.
- `status` outside `draft` / `active` / `completed` / `archived` imports as
  `draft`, reported in `fields`.
- Metadata is validated against the target section's declaration, so a value the
  import accepts is one the section would have accepted anyway. YAML resolves an
  unquoted scalar to a type, so `due: 2024-01-02` arrives as a date and is handed
  to the validator as `2024-01-02` — the shape a `date` field expects and the
  shape the download writes. Numbers stay numbers, because `number` is the one
  type checked numerically; everything else is rendered as a string.
- The entry is created by the ordinary path, so its cross-references are
  extracted from the stored content as usual and **revision 1 is attributed to
  `import`** — the first time that author kind is written at document
  granularity. See [Revisions](/llms/revisions.md).

### What survives the round trip, and what does not

Download a document with `GET /api/entries/{id}/markdown`, drop the file back in,
and this is the contract:

| Survives | Does not, on purpose |
|---|---|
| `title` | the `E`-code — codes are issued by the research and are unique inside it, so the copy gets a new one |
| `description`, when a person wrote one (a derived one is re-derived from the body) | `[[E3]]` and every other cross-reference — the text is preserved exactly, but it resolves against the **destination** research, not the source |
| `status` | provenance: session, author, revision history. The copy starts at revision 1, authored `import` |
| `tags` | `created` / `updated` — the copy is a new document and says so |
| declared `metadata`, revalidated against the destination section's `field_spec` | the section and research the file names, which are only a `warnings` line |

**The asymmetry is the design, not a bug.** A code means nothing outside the
research that issued it, so honouring `code: E50` would either collide with a
real E50 or invent an identity. A `[[E3]]` written in R99 names R99's third
entry; repairing it here would mean guessing what it meant somewhere else. So the
import lists the dead references and repairs nothing. Import into the **same**
research and the references resolve again, because the codes they name are still
there — that, and not identity, is what makes a round trip look lossless.

A note taken out of an **Obsidian vault** is a worse round trip and worth stating
separately: the vault rewrites `[[E3]]` to `[[E3 — Pricing model|E3]]` for
Obsidian's benefit, and nothing here reverses that, so each one arrives as an
unresolved reference. The vault also writes no `description` and does write
`aliases` and `session`, which are ignored and refused respectively.

### There is no MCP tool

Deliberately, and this is the note an agent is meant to find here rather than go
hunting for a tool. **An agent writes with `entry_create`** — it already has the
content, and it can pass `title`, `description`, `status`, `tags` and `metadata`
directly, with none of the parsing, none of the guessing and none of the
refusals. Dropping a file is a human act: the file is on a person's disk, and
the preview exists so *they* can see what would be lost before committing. If a
user asks, point them at the section page — the **Import .md** button beside the
view toggle, or a drag-and-drop onto the entry list. Nothing about the tool list
changes because these routes exist.

## Portable Export / Import

The markdown/JSON exports above are for reading. To move a research to another server, use the portable format instead — it carries sections, entries, sessions, questions, tasks, roadmaps and the annotations on each document, in a versioned envelope (`version`, `exported_at`, `research`):

```
GET  /api/researches/{id}/export/portable   -> portable JSON (downloaded as <name>.json)
POST /api/researches/import[?team={id}]     -> body is that JSON, returns the new research_id and code
```

The `research_export` MCP tool returns the same portable payload for a research ID or short code — that is what `format` defaults to.

Import re-creates entities from scratch: new UUIDs, new short codes, cross-references re-parsed from the imported content.

**Section declarations and document metadata do travel.** `field_spec` rides on
the section and `metadata` on the entry, and an import restores both — otherwise
an imported research would arrive as a pile of values nothing explains. Two
consequences: a declaration the destination would not have accepted (a reserved
key, a cap breached, an enum with no options) is dropped whole rather than
enforced half-way, so the section lands as a plain topic and its documents' values
land as orphans; and values are re-validated against whatever the destination
section declares, because a dump carries the values, not the authority that
collected them. `spec_version` is not carried — an imported section that keeps
its declaration starts at version 1.

**Where the import lands.** The new research goes into the caller's personal team unless another one is named: `?team={id}` on the REST route, `team_id` on the `research_import` tool. Naming a team you are not in is `not found`; naming one where you are only a `viewer` is refused with `your role in this team does not allow this`. Ownership does not travel with the payload — the export carries no team and no user.

**Private skills and memory travel in portable version 2.** Exports emit version
`2`; imports accept `1` and `2`. Structured memory carries text, author, nullable
creation time and optional session code. Local memory IDs and session UUIDs are
cleared on export; import creates fresh item IDs and versions and remaps session
codes to the new sessions. Version 1 string notes become items with unknown
authors and null creation times. Legacy `instruction` becomes an attached
private skill marked `needs_trigger`, preserving its full text.

`private_skills` carries each research-owned skill's slug, name, description,
body, `needs_trigger` and `attached` state, including detached private skills.
Import restores these with fresh IDs. Team and built-in libraries and their
attachments do not travel; re-attach reusable skills on the destination. The
Obsidian vault includes private skill bodies and memory with provenance in its
README for authenticated exports. Share exports exclude both memory and skills.

**No template provenance either.** `template_slug` and `template_version` are on the research record but not in `ExportResearch`, so an import lands with no methodology recorded — and could not honour one anyway: a slug names a row in *this* server's template library, and the destination may have a different set, a fork under the same slug, or none. The stamp says which methodology this research was started from on this server; it is not a portable reference. If it matters to the reader, record it in a private skill or an entry, which do travel. See [Templates](/llms/templates.md).

**No history travels with an export.** Revisions are not in the portable payload, and every entry an import creates starts at revision 1 attributed to `import` rather than to an agent that never wrote it. Export a research, import it elsewhere, and who wrote what before the export is only in the original server. The vault's `_history/` tables (`revisions=true`) are a readable record, not a transferable one — nothing imports them back.

## Export Through a Share Link

A [read-only share link](/llms/domain-guide.md#share) may carry the research export, and only if the link was created with `include.export`:

```
GET /api/shared/{token}/researches/{id}/export
GET /api/shared/{token}/researches/{id}/export?format=md
GET /api/shared/{token}/researches/{id}/export?format=obsidian
```

Same handler, same payload shape as the authenticated route above, with four differences that are the point of the feature:

- Private skills and memory are absent, along with `user_id` and every team field. Research reads redact memory and ownership; export code excludes private skills for a share context. No shared format carries them out.
- **A share sees neither document metadata nor the declaration behind it.** `field_spec` is stripped from every section a share reads and `metadata` from every entry, so the shared markdown renders no metadata block and the shared vault emits no user front-matter keys — both render *from* the declaration, and a share has none. A list of field labels with nothing in them still says what a team decided to track. See [Document Metadata](/llms/metadata.md).
- `sessions` is empty unless the link includes sessions, and `tasks` unless it includes tasks. An export that carried the interview transcript would hand over in one file exactly what the creator chose to leave out of the pages.
- `/export/portable` is not mounted under the prefix at all: it is a re-importable copy of the record rather than a reading of it. **Session export is not shared either** — only the research-scoped route is mounted.
- **The per-document download is not mounted either.** `GET /api/entries/{id}/markdown` has no shared twin, and no include flag turns one on. The service cannot refuse that route on a visitor's behalf — a share resolves to viewer on its research, so the document reads fine and the file would render — so the route list *is* the guarantee, which is why a test drives the real mux and asserts the `404`.

### The vault through a share

`?format=obsidian` works on a link that includes downloading, and the archive is narrowed to what that link publishes. The narrowing happens in `service.clampForShare`, next to the code that reads the options rather than in the handler — the vault's parts come from a query string the visitor can type, and the include flags gate *routes*, which is a check the vault never passes through.

| Requested | What a visitor gets |
|---|---|
| `sessions=true` | only if the link includes sessions |
| `tasks=true` | only if the link includes tasks |
| `roadmaps=true` | only if the link includes roadmaps |
| `revisions=true` | never — no flag publishes an entry's history |
| — | the `session:` key in an entry's frontmatter is dropped, and the `Session:` footer link with it |

Revisions and provenance are refused outright rather than gated, for the same reason the shared entry pages omit them: who edited what, when, and from which session is working process, like private skills.

The research itself needs no special handling — the vault builds from `ResearchService.Get`, which redacts memory; private skills are separately excluded for share contexts, so it starts from the published research. This route was a `404` for a while on the belief that it did not; it is `internal/api/share_routes_test.go` that now settles the question, by unzipping the response and asserting against filenames.

Without `include.export` the route answers the same `404 this link is no longer available` as a revoked token: a link that does not offer a download should look like a server that has none.

## Auth

Export endpoints are read endpoints: unauthenticated by default, but they require a bearer credential — a JWT, an API key or an OAuth access token, which are interchangeable — when `auth_enabled` is set, and they only ever see researches owned by a team the caller belongs to — a research in someone else's team is `404`, indistinguishable from one that does not exist. **Exporting needs no more than read access**: a `viewer` may export a whole research, a session, the Obsidian vault, or one document as a file, exactly as an `editor` can. The two import endpoints are writes: they always require the bearer token when `api_token` or `auth_enabled` is configured, and with neither configured they take no credential but are accepted only from the machine the server runs on — a remote import answers `401 the write API is disabled`. `POST /api/researches/import` needs editor or owner rights in whichever team it imports into; `POST /api/sections/{id}/import` and its `/preview` need them in the team that owns the research holding that section — the preview included, even though it writes nothing.

A share token is not a bearer token. It authenticates nothing on the routes above — it only opens the mirrored, redacted export under `/api/shared/{token}/…`, and only when the link includes it.

## Block Documents in Export

An entry with `entry_type: blocks` stores a JSON document of typed blocks (see
[Block Documents](/llms/blocks.md)), so every export has to render it rather than write
`content` out:

- **Markdown export** (`?format=md`, the `markdown` field of the JSON responses,
  and the single-document download) serializes the blocks: headings, lists,
  tables, quotes and code become their markdown equivalents, a `callout` becomes a labelled blockquote, a
  `mermaid` block becomes a ```mermaid fence with a link to mermaid.live below it, a `checklist` becomes a GitHub
  task list carrying the ticks as they stand, and a `transcript` becomes one
  paragraph per turn — `**Peter** *(00:03:12)*: text`.
- **A `task_ref` block is resolved against the tasks the export already loaded.**
  It becomes a GitHub task list of real titles and statuses — `- [x] T4 — Title`,
  under an `*1 of 3 done*` line — and falls back to a plain list of `- [[T4]]`
  references wherever there is no task list to resolve against: a revision diff,
  and a vault or session export through a share link that publishes no tasks.
  Nothing here reads tasks on its own, so an export that was refused the task list
  cannot leak a task title through a document.
- **An `html` block is named, not emitted.** The export gets
  `*<title> — interactive HTML, view in the web UI.*` and its caption. A whole
  HTML document in a markdown file is not readable, is not markdown, and inlined
  it would leak its `<style>` and `<script>` into whatever renders the file. The
  same applies to legacy `artifact` rows written before the type was folded into
  the `html` block.
- **The Obsidian export is the exception on both counts.** One serializer renders
  a block document everywhere, with two overrides for the vault: the mermaid.live
  link under a diagram is dropped (Obsidian draws the fence itself), and an `html`
  block is written as a real file under `_html/` — wrapped in `<!doctype html>`
  when the block is a fragment — with a callout in the note linking to it. It is
  the one export where the HTML survives.
- **JSON responses** (`GET .../export` and `GET .../sessions/{sessionId}/export`
  without `format=md`) carry `entry_type` next to `content`, and for a blocks
  entry also `content_markdown` — the same rendering the `.md` file uses. Read
  that field and you need no knowledge of the block format.
- **A document that cannot be parsed** yields
  `*This entry holds a block document that could not be read.*` rather than the
  raw JSON or a silent gap.
- **Portable export** carries `entry_type` on each entry, so a blocks entry
  imports as a blocks entry, and the import validates the document before writing
  anything. Exports written before entry types existed have no `entry_type`; those
  entries import as `markdown`, which is what they were.

## Cross-References in Export

Cross-references (`[[E3]]`, `[[R2:E5]]`, `[[RM1]]`) are preserved as-is in markdown export. In the web export pages, they are rendered as clickable links.

The Obsidian vault is the one export that rewrites them: each reference is retargeted at the note's filename and keeps its code as the display text (`[[E3]]` becomes `[[E3 — Pricing model|E3]]`), because Obsidian matches a wikilink against filenames and an `aliases` entry does not resolve a hand-written one. See [Obsidian Vault Export](#obsidian-vault-export) above for the full rule and what happens to a reference pointing outside the export.

The [single-document download](#one-document-as-a-file) is the case worth stating twice: it produces an Obsidian-shaped file, front matter and all, and still does **not** rewrite its references — the notes they would name are not there. Dropped into a foreign vault, every `[[E3]]` in it resolves to nothing.
