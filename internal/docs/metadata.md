# Document metadata

A section declares the fields its documents record. An entry carries the values.

This exists because the alternative is what production research actually looked
like: eighteen specifications in one section, each opening with five lines of
hand-typed prose — status, date, which services produce and consume the thing.
None of it was queryable, and the prose said "Status: draft for review" while the
entry's stored `status` was `active`. They disagreed, nothing detected it, and
the reader believed the prose, because prose is at the top of the page while
`status` is a chip in the chrome.

A field is worth declaring only when it is **the only place its fact is written**
and it can be filtered on. If prose can restate it, prose wins and the field is
dead weight.

## The vocabulary is closed

Only a key the section declares may be written. A key it does not declare is
reported back and **dropped**.

**A section that declares nothing accepts no metadata at all**, and that is the
normal case. Most sections are topics, not classes of document; they declare
nothing and behave exactly as they did before this feature existed. Do not
declare fields on a section because you can. Declare them when the section holds
one *kind* of document repeatedly.

## Metadata is not tags

They are not alternatives and neither replaces the other.

| | `tags` | metadata |
|---|---|---|
| vocabulary | free-form, invented as you go | fixed keys, declared by the section |
| scope | cross-cutting, spans sections and researches | per-section, one class of document |
| shape | a flat list of strings | typed slots: enum, ref, date, text, number, url |
| answers | "what is this about" | "what does this document record" |

Never write a system fact into metadata. `status`, `tags`, `created`, `updated`,
`session` and six more keys are **refused at declaration time** for exactly that
reason — see Reserved keys below. If authors keep inventing states the entry
`status` enum lacks, that is an argument for a section-declared `stage` field
with the section's own vocabulary sitting *beside* `status`, never for writing
`status: draft` into metadata.

## Declaring the fields

`section_update` (MCP) or `PUT /api/sections/{sectionId}` (REST), field
`field_spec`. **There is no other way in.** `research_create` and
`research_add_section` do not take a declaration in either transport; create the
section first, then declare on it. (A portable import is the one exception — it
carries the declaration with the section.)

```json
{
  "section_id": "uuid — an S-code is not resolved here",
  "field_spec": [
    { "key": "stage",    "label": "Stage",    "type": "enum", "required": true,
      "options": ["draft", "in review", "approved", "superseded"],
      "help": "The state in the review workflow; ask if unclear" },
    { "key": "produces", "label": "Produces", "type": "text", "required": false, "repeated": true },
    { "key": "spec_ref", "label": "Registry", "type": "ref",  "required": true,
      "help": "The registry entry this specification implements; ask if unclear" }
  ]
}
```

- `field_spec` **replaces the whole declaration.** Omit it (or send `null`) to
  leave it alone. Send `[]` to remove every field — which never deletes the
  values documents already carry under those keys.
- Inside a field object, `key`, `label`, `type` and `required` must be present;
  `repeated`, `options` and `help` may be omitted. `options` is nullable;
  `repeated` (boolean) and `help` (string) are **not** — send `false` and `""`,
  never `null`.
- `key` is an immutable identifier, `label` is what gets renamed. Changing a key
  is a removal plus an addition, and the values under the old key become
  orphaned. Spell it that way on purpose or not at all.
- `key` must match `^[a-z][a-z0-9_]*$`.

**Say the same thing in the section's `instruction`.** The same call carries
`instruction` — three to six lines of imperatives read before every document
filed here, at most 500 runes, refused rather than truncated. Where a section
declares fields, its instruction should name those keys: the declaration says
which slots exist and what a valid value is, the instruction says how to arrive
at one and what the prose around them must do. Left to drift, they end up
describing the same document differently, which is the disagreement this whole
feature exists to remove. Keep it to what a document *here* looks like —
research-wide tone and methodology belong in the research's memory or in a
[skill](/llms/skills.md), which is also where the precedence between the three
is written down.

### Field types

| `type` | Accepted value | Stored as |
|---|---|---|
| `enum` | one of `options` | the string. A value outside the list is stored and flagged, not refused |
| `ref` | a short code — `E47`, `R2:E5`, `RM1`, `RM1:N3`, `R2` | the bare code; surrounding `[[ ]]` are stripped |
| `date` | `YYYY-MM-DD`, or an RFC3339 timestamp | `YYYY-MM-DD` — a timestamp is truncated |
| `text` | one short line | trimmed, newlines and tabs collapsed to spaces |
| `number` | a number, or a numeric string | a number |
| `url` | an `http` or `https` URL with a host | the string |

`repeated: true` on any of them takes a list. A bare single value is accepted for
a repeated field and wrapped — an author writing one service name does not have
to know it is a list.

There is no `boolean`: absent and `false` are indistinguishable to a reader, so a
two-value `enum` with real words says more.

**Prefer `enum` wherever the answer can be enumerated.** It is the only type that
measurably raises how often a field gets filled — a field converted from free
text to four to six named options fills several times better, while `date` and
`text` fill no better than untyped. Types help exactly insofar as they narrow the
answer space.

### Caps

| Cap | Value |
|---|---|
| fields per section | 12 |
| required fields per section | 5 (a separate, harder ceiling) |
| `enum` options | 20 |
| values in a repeated field | 20 |
| value length | 200 runes (counted in runes, so Cyrillic is not worth half) |
| `label` / `help` | 60 / 200 characters |
| `key` | 32 characters |

The field cap protects legibility, not the database: past a dozen the block stops
being scannable and the reader goes back to believing the prose. Just past the
cap people start encoding structure in field names (`contact_1`, `contact_2`) and
metadata begins carrying content.

`GET /api/metadata/schema` serves this table, the type catalogue and the reserved
keys as JSON, so a client never carries a second copy that drifts. Three caps
that belong to no field ride along in the same payload — `import_max_bytes` and
`import_extensions`, the limits on a markdown file dropped into a section, and
`instruction_max`, the 500 runes a section's writing instruction may run to — for
the same reason: a number hard-coded in the frontend is a lie the day it changes
on the server. See [Export](/llms/export.md).

### Reserved keys

Refused outright: `code`, `title`, `aliases`, `research`, `section`, `type`,
`status`, `tags`, `created`, `updated`, `session`.

These eleven are what the Obsidian export already emits as YAML front matter.
YAML is last-wins, so a user field keyed `status` would silently overwrite the
system value in every exported note, and one keyed `code` or `aliases` would
break every `[[E3]]` in the vault. The refusal lands when the declaration is
written, not when a note is rendered months later.

### What a bad declaration does

Unlike a value write, a bad declaration **is** refused — every problem at once,
so a form can show them all: `invalid field_spec: fields[0]: key "status" is
reserved …; at most 5 fields may be required, got 6`. REST answers `400`.

### `spec_version`

Bumped by one whenever the declaration actually changes. Saving an unchanged
declaration does not move it. Entries record the version their values were last
validated against, so a reader can tell which rule a document was held to —
sections are overwritten in place and would otherwise silently restate history.

## Writing values

`entry_create` and `entry_update`, field `metadata`; `POST /api/entries` and
`PUT /api/entries/{id}` the same.

```json
{ "metadata": { "stage": "in review", "produces": ["scanner-watchdog"], "spec_ref": null } }
```

- On `entry_update`, `metadata` **replaces** the values. Omit it (send `null`) to
  leave them alone; send `{}` to clear them.
- **`null` as a value is an explicit unknown, and it answers a required field.**
  An absent key means nobody recorded anything; `null` means somebody looked and
  could not say. This distinction is the point: a model, unlike a person, does
  not leave a field blank when it does not know — it fills it plausibly. It will
  write `owner: platform-team` because the document mentions a platform. **Send
  `null`. Do not guess.**
- An empty string clears the field rather than storing something that reads as an
  answer.
- A `[[E3]]` inside a value is **not** indexed as a cross-reference. Use a `ref`
  field for a link you want typed, and write real cross-references in the
  document body.
- Values are single-line: newlines and tabs collapse to spaces, and a literal
  `\n` stays a backslash and an `n`, as it does in a title.
- Do not hand-maintain a `related` field. Cross-references already compute that
  graph for free, and every vault that added a second copy ended with the two
  disagreeing. Same for a hand-typed date next to a derived `updated`.

## Nothing fails a write on the interview path

No metadata problem refuses a write on the interview path — every MCP tool, every
REST entry write, every portable import. The author is usually a model in the
middle of an interview, and a rejection there destroys answers a person has
already given. (The two exceptions are both outside it: completing a document
with required fields unanswered, and importing a markdown file — see below.)
Instead the response carries `metadata_report`:

A write against the declaration above that sent a `stage` outside the vocabulary,
a good `produces`, an undeclared `owner` and no `spec_ref` at all comes back with:

```json
"metadata_report": {
  "spec_version": 3,
  "stored": ["produces", "stage"],
  "unknown_keys":   [{ "key": "owner",  "reason": "this section does not declare a field with this key" }],
  "invalid_values": [{ "key": "stage",  "reason": "\"sent for review\" is not one of: draft, in review, approved, superseded" }],
  "missing_required": ["spec_ref"]
}
```

- `unknown_keys` were **dropped**. That is what the closed vocabulary means at
  the write path.
- `invalid_values` were **stored verbatim** and flagged — which is why `stage`
  appears in `stored` as well. Discarding a person's value to protect a type is
  the same mistake as refusing the write.
- `missing_required` names declared required fields the document still does not
  answer.
- The key is absent entirely when the write carried no metadata. `spec_version`
  is the section's version the values were checked against.
- Over MCP the report gains a `hint` when — and only when — `missing_required` is
  non-empty: fill them next time, or send `null`, but do not guess.

**Read the report.** An entry that comes back looking fine may have had half its
metadata dropped, and this is the only place that says so — the same shape and
the same reason as `block_report` on a blocks entry.

## The one gate: `completed`

Moving an entry to `status: completed` while required fields are unanswered is
refused. Every other write is accepted — with one exception outside the
interview, below.

- MCP: `entry_update` returns an error result — `cannot complete: required
  metadata is unanswered: stage, spec_ref`.
- REST: `409` with `{"error": …, "code": "metadata_incomplete",
  "missing_required": [...], "hint": …}`.

The override is `allow_incomplete: true` on the same call — a decision, not a
retry. Fill the fields, or send `null` for the ones nobody can honestly answer,
before reaching for it.

The gate is on the transition. It does not fire when the entry is already
`completed`, and it is checked against the section's declaration **as it stands
now**, not the version the entry was written under: whether a document may be
called finished is a question about the rules in force today.

### The exception: importing a file

`POST /api/sections/{id}/import` is the one write that **refuses** on metadata:
any `unknown_keys` or `invalid_values` and the commit comes back `400 this
document cannot be imported as given: …`. The asymmetry is about who is holding
the keyboard, not about the data. Everywhere else the author is a model in the
middle of an interview and a rejection destroys answers a person has already
given; there, a person is standing over the file with an undo, and the preview
step has already shown them exactly these problems. `missing_required` is **not**
a refusal even there — an imported document may be incomplete like any other. The
preview's report carries a `value` on each issue, saying what the file actually
claimed, which no other write does. See [Export](/llms/export.md).

## Reading it back

| Call | What you get |
|---|---|
| `section_list`, `research_get` | `spec_version` on every section, `field_spec` **only when the section declares something** — an empty declaration is omitted rather than sent as `[]` — and `instruction` on the same terms, only when the section has one |
| `entry_read`, `GET /api/entries/{id}` | the entry's `metadata`, its `spec_version`, and `metadata_status` |
| `entry_list` | **neither.** It is deliberately content-free; read the entry to see its values |
| `GET /api/metadata/schema` | the type catalogue, the caps and the reserved keys |

`metadata_status` is computed on every read against the section's current
declaration and never stored — a stored completeness flag goes stale the moment
the declaration changes:

```json
"metadata_status": {
  "complete": false,
  "missing_required": ["spec_ref"],
  "orphaned": ["legacy_owner"],
  "issues": [{ "key": "stage", "reason": "\"sent for review\" is not one of: …" }],
  "spec_version": 4
}
```

`issues` re-checks stored values so the reader who can fix one is the one who is
told. `spec_version` here is the *section's* current version, which may be ahead
of the entry's — that difference is exactly what topping up works through.

## The declaration is a lens; values outlive it

A schema edit never rewrites a document.

- **Adding a field** makes existing documents incomplete without touching one of
  them. Incomplete, never invalid: a missing element has never made a record
  inauthentic, it annotates it.
- **Removing a field** keeps every value collected under it. They show up in
  `metadata_status.orphaned`. Deleting a field is a decision about future
  collection, not a verdict on what was already written.
- **Changing a type** never coerces stored values. They surface in
  `metadata_status.issues` until someone rewrites them.

**Topping up happens on the next ordinary write, and that is the whole migration
mechanism.** When you touch an entry whose section has grown a field, fill it
then. Nothing runs a bulk migration; nothing needs to.

## Choosing fields well

Four things put a field in the mode that actually gets filled, in order of force:

1. **It is visible in a view its author uses.** A blank cell next to filled ones
   is the strongest fill mechanism there is.
2. **The value is already in the author's head at writing time.** `produces` at
   spec time is free; `last_reviewed` is free at no moment.
3. **The type narrows the answer.** Push toward `enum`.
4. **A sibling document has it filled.** Copying is the real authoring
   behaviour, and an agent amplifies it — so **the first two or three documents
   in a section set the pattern for every one after.** Read the section's
   existing documents before writing into it.

Required fields are permitted, rare, and equipped: at most five, and every one
should carry a `help` line saying **where the value comes from** ("the service
name from the repo; ask if unclear"). The agent reads it. A required field with
no provenance note is an invitation to invent.

## Where it shows up

**Markdown export** (`GET /api/researches/{id}/export?format=md`) renders a
labelled block above each document's body, in declared order. A declared field
with no value prints an em dash rather than being skipped — a reader of the file
has no other way to learn the field exists and nobody answered it, and that gap
is what "incomplete" looks like on paper. Values under keys the section has since
dropped are kept in the database but not printed. The session export does not
render metadata.

**Obsidian vault** (`?format=obsidian`) emits user keys as front matter *after*
the eleven system ones, so two notes scan the same way. A declared field with no
value is written as an explicit `null`, not omitted — a vault query for
"documents missing this field" would otherwise find nothing, and the documents
worth finding would be exactly the invisible ones. A `ref` value is emitted in
its `"[[E47]]"` bracket form so Obsidian treats the property as a link; a
repeated field is a real YAML sequence even with one element, so a membership
filter works; a number stays unquoted so it sorts numerically. A field answered
with an explicit `null` emits the word `unknown` rather than a blank — somebody
looked, and that is a different fact from nobody having looked.

**One document downloaded as a file** (`GET /api/entries/{id}/markdown`) carries
the same front matter from the same builder: declared keys after the system ones,
`null` for an unanswered field, `unknown` for an explicit one. Two of the vault's
system keys are absent there (`aliases` and `session`), and one the vault does not
write is present — `description`, and only when a person wrote one rather than it
being derived from the body. Nothing else differs; one builder, because two would
drift and nobody would notice until they diffed two exports of one document.
Dropping that file back into a section revalidates its declared keys against
whatever the destination section declares. See [Export](/llms/export.md).

**Portable export/import** carries both `field_spec` on the section and
`metadata` on the entry, and an import restores them. Values are re-validated
against the destination section's declaration, which may be a different one — a
dump carries the values, not the authority that collected them. A declaration
that would not have been accepted here is dropped whole rather than enforced
half-way: the section arrives as a plain topic, and its documents' values arrive
as orphans.

**Revisions.** Metadata is part of the snapshot, so an edit that changes only
metadata still leaves a revision — without it the edit would be judged a no-op
and vanish rather than merely go unrecorded. The change is reported one row per
key — `metadata.stage`, not one row saying an object changed — in the `fields`
list of `GET /api/entries/{id}/diff` and `GET /api/sessions/{id}/changes`. The
MCP `entry_diff` tool returns the content diff only and carries no `fields`, so
a metadata-only edit reads there as a revision that changed nothing; read the
entry, or the REST diff, to see it. Revision 1 of an entry that predates this
feature reports empty metadata, which means "predates the feature" rather than
"was empty". See [Revisions](/llms/revisions.md).

**Share links** see neither the values nor the declaration. A list of twelve
field labels with nothing in them still says what a team decided to track, and
the values are exactly the facts a section spec invites a team to record — an
owner, a cost, an interviewee, an internal ticket. Both are stripped on the read
path, so no shared page, export or vault carries them out.
