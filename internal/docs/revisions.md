# Revisions

Every write that changes what an entry says appends a snapshot. Nothing is
overwritten in place any more: the entry as it stood before your edit is still
readable, still diffable, and can be restored.

This exists because entries are written by models across many sessions. The
third session can quietly make a document worse, and without a history nobody
can prove it happened or get the earlier text back.

## What a revision holds

| Field | Meaning |
|-------|---------|
| `revision` | 1-based, per entry. Never reused. |
| `title`, `description`, `content`, `entry_type`, `status`, `tags` | The entry exactly as it stood after that write |
| `metadata`, `spec_version` | Its section-declared field values after that write, and the declaration version they were checked against — see [Document Metadata](/llms/metadata.md) |
| `author_kind` | `agent`, `human`, `import` or `restore` — see below |
| `session_id` / `session` | The session that was **active when the write happened** |
| `summary` | A short label: "Updated content, tags", "Patched blocks: inserted 2" |
| `created_at` | When it was written |

`content` is the one field a list never carries: `entry_history` and
`GET /api/entries/{id}/revisions` return metadata only, because a history of a
long entry made of copies of it is unreadable and expensive. Read one revision's
content with `GET /api/entries/{id}/revisions/{n}`, or `entry_diff` for what
changed between two.

`author_kind` is the field a reader looks at first. "A person wrote this" and "a
model wrote this" are different claims, and until this existed the product could
not tell them apart. The credential is the evidence:

| Kind | Written by |
|------|-----------|
| `agent` | Every MCP write, and a REST write carrying an API key, an OAuth token or the legacy write token |
| `human` | A REST write from a browser session (a JWT) — and one with no credential at all, where the only thing on that port is the web UI |
| `import` | Either import: a whole research (`research_import` / `POST /api/researches/import`) or one markdown file dropped into a section (`POST /api/sections/{id}/import`) |
| `restore` | The system putting an earlier revision back |

In the MCP result the field is called `author`, and the session comes back as
`session` holding the `SS`-code. `author_kind`, `session_id` and `session_code`
are the REST spellings of the same three things.

## Not the same as a block document's `rev`

A `blocks` entry carries a `rev` — a content hash used to reject a patch written
against a document that has since changed (see [Block Documents](/llms/blocks.md)).
That is optimistic concurrency, for one write.

A **revision** is a numbered snapshot in time. Different jobs, unfortunately
similar words. `rev` is a hash like `9f2c1a44be07`, returned by `entry_read` and
`entry_patch` and sent back to `entry_patch`; a revision is a number like `4`,
returned by `entry_history` and sent to `entry_diff`. Neither is accepted where
the other belongs.

## What does and does not create one

Created:

- `entry_create` — revision 1, the entry as it was first written
- `entry_update` — any change to title, description, content, type, status, tags or document metadata. A write that touches only metadata still appends a revision: without metadata in the snapshot such an edit would be judged a no-op and disappear rather than merely go unrecorded
- `entry_patch` — any structural change to a block document
- import — revision 1, attributed to `import`, whether the whole research was imported or a single markdown file was dropped into a section
- restore — a new revision holding the restored content

Not created:

- **A write that changes nothing.** Rewriting identical text three times leaves
  one revision, not three.
- **A checkbox tick** (`entry_patch` with only `set_state` ops). Ticking a box is
  not an edit to the document, and recording it would bury the writes that
  changed what the entry says under a history of clicks.

An entry that predates this feature carries one backfilled revision: number 1,
`author_kind: agent`, no summary, timestamped with the entry's last update rather
than its creation. A one-row history like that is the absence of a record, not a
claim that the entry was written once. A revision written before document
metadata existed reads as empty metadata, which likewise means "predates the
field" rather than "was left blank": no backfill can invent values nobody
collected.

## Tools

`entry_history(entry_id, limit?)` — the revision list, newest first: `revision`,
`author`, `created_at`, `title`, `status`, `entry_type`, `tags`, `description`,
plus `summary` and `session` (the `SS`-code) where they are known. **No content.**
`limit` defaults to 20 and may be omitted; when more revisions exist the result
carries `truncated: true`.

`entry_diff(entry_id, from?, to?)` — a unified diff between two revisions.
`to` defaults to the newest revision and `from` to the one before it, so a call
with neither shows the most recent change. The result carries `from`, `to`,
`author`, `changed_at`, a `summary` of the shape `+12 −3`, the `added` and
`removed` line counts, and `diff`: the classic `+`/`-` text with three lines of
context. A changed title comes back as `title_before` / `title_after`. Both
arguments are revision **numbers**, never a `rev` hash.

**Read the history before rewriting an entry another session wrote.** It is the
cheapest way to avoid undoing someone's correction, and `entry_diff` will show
you exactly what the last session changed and why the document looks the way it
does.

Block documents are diffed through their **markdown projection**, never their
JSON: what changed is a paragraph, not a field inside an object.

Document metadata is reported **one row per key** — `metadata.stage`, not one row
saying an object changed. A key present on one side only shows as an empty half,
which is the truth: the value was not recorded then. Those rows live in `fields`,
which only the REST diff and `GET /api/sessions/{id}/changes` return: the MCP
`entry_diff` tool carries the content diff and a title change and nothing else,
so a revision that only flipped a status, a tag or a metadata value reads there
as a change to nothing.

Two documents of more than 4000 lines each are not aligned line by line; the
comparison falls back to "everything was replaced" and says `truncated: true`.

## REST

```
GET  /api/entries/{id}/revisions               list, newest first, no content
GET  /api/entries/{id}/revisions/{n}           one revision, with content
GET  /api/entries/{id}/diff?from=3&to=5        both bounds optional
POST /api/entries/{id}/revisions/{n}/restore   restore, as a new revision
GET  /api/sessions/{id}/changes                everything a session created or changed
```

`GET /api/entries/{id}` needs none of these to say who last touched the document:
it carries the newest revision beside `data`, as `revision`, `author_kind`,
`revised_at`, `revision_session` and `author_name` — the writer's name, or their
email when they have set no name, resolved from the credential that wrote the
revision and therefore present for an `agent` write as much as a `human` one.
It is absent when there was no user (auth off) or the account is gone; nothing
about a document fails to render because its author can no longer be looked up.
**A share visitor gets none of that block**, the name least of all.

`{id}` on the entry routes is the entry **UUID** — unlike `GET /api/entries/{id}`,
they do not resolve an `E`-code. The session route takes a UUID or an `SS` code.
The four `GET`s are read endpoints: unauthenticated by default, bearer token
required when `auth_enabled` is set, and readable by any member of the owning
team — a `viewer` may read history and diffs. The restore is a write: it needs
the token whenever `api_token` or `auth_enabled` is configured, and an `editor`
or `owner` role. A `viewer` restoring gets `403` /
`your role in this team does not allow this`. With neither `api_token` nor
`auth_enabled` configured it needs no credential but is accepted only from the
machine the server runs on, like every other write in that mode — see
[MCP Client Guide → Connecting Over HTTP](/llms/mcp-client-guide.md#connecting-over-http-which-credential-this-instance-wants).

## Restoring

Restoring revision 2 onto a document at revision 7 produces revision **8**.
History is append-only: revision 7 is still there, and restoring is itself
undoable by restoring it. Restoring the newest revision changes nothing and so
appends nothing — the no-op rule above applies to a restore as well.

Two things survive a restore that you might expect to be rolled back, both
deliberately:

- **Checkbox state.** The ticks belong to whoever made them, not to the prose an
  agent wrote around them. Restoring the text of a document does not untick a
  human's work.
- **Block ids.** They come back with the restored document, so anything bound to
  a block stays bound to it — a person's [annotations](/llms/annotations.md)
  included, which re-anchor against the restored text like any other read.

## An annotation points back into the history

Every mark a person leaves on a sentence records `anchored_revision`: the
entry's revision at the moment they made it. That number is the whole reason an
orphan is investigable rather than merely broken.

When `annotation_list` returns a mark whose anchor state is `orphaned` — the
block is gone and the quote is nowhere in the document — the text somebody
doubted was rewritten or deleted, and the mark still carries the sentence as it
read. Do not guess what happened to it:

1. `entry_history(entry_id)` — the writes since, and who made them. A revision
   authored `human` between then and now is a different situation from a chain
   of your own.
2. `entry_diff(entry_id, from: anchored_revision, to: <newest>)` — what actually
   became of the paragraph. `from` is the anchored revision, a **number**, never
   the `rev` hash a blocks entry carries.

Then answer with what you found. "The claim was removed in revision 9 when the
section was rewritten" is a real answer to a mark on a sentence that no longer
exists; re-asserting the claim so the mark has something to sit on is not.

The field is absent from the tool response when it is `0`, which means no
revision was recorded at the time — an entry old enough to predate the history,
not a mark on nothing. There is then nothing to diff from, and the honest move is
a question.

## What a session changed

`GET /api/sessions/{id}/changes` reports every entry a session created or
edited, with the revision range and a diff — including entries it edited without
creating, which the "entries produced in this session" list has never covered.
The web UI renders it as the **Changes** tab on a session page.

## History size

Every revision is kept by default. An entry is kilobytes of markdown or JSON,
so keeping everything is both cheap and the honest choice for a research tool.
An operator may set `revision_limit` in YAML,
`MCP_RESEARCH_REVISION_LIMIT`, or `--revision-limit` to cap each entry's
history. The cap keeps the newest N **plus revision 1** — the only record of
what the entry looked like when it was created — and every revision that is
still a reader's personal last-seen checkpoint. Keeping those checkpoints is
what lets the web UI show an exact diff from what each reader last saw even when
older, otherwise-unused snapshots have been trimmed.

Deleting an entry deletes its history with it (the rows cascade), and so does
deleting the research. History does not travel: a portable export carries no
revisions, and every entry an import creates starts again at revision 1,
attributed to `import` — the same is true of a single document downloaded as a
`.md` file and imported back, which is a new document with a new code and no
history rather than the original returning. The Obsidian vault export can write
history out as a readable table per entry (`?format=obsidian&revisions=true`, one
`_history/{code}.md` each), but nothing reads it back — see
[Export](/llms/export.md).

A file cannot bring history with it either. The markdown import **refuses**
`session`, `author`, `author_kind`, `revision`, `revisions` and `history` in
front matter, reporting each one rather than using it: front matter is text and
can assert anything, and everything this product says about who did what and when
is its own record. That is a trust rule, not a fidelity one.

A **share link never carries history**, and `revisions=true` on a shared vault is
ignored rather than honoured. There is no include flag that turns it on: who
edited an entry, when, and from which session is working process, and a link
publishes findings.
