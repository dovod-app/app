# Handoff — issue #53, delete a research

**Branch** `feat/research-delete` · **Worktree**
`/home/butschster/repos/mcp/mcp-research/.claude/worktrees/research-delete` ·
**Base** origin/master, merged at `15b4adb` (carries #115, #116, #117).

Frontend deps are installed in this worktree, `vue-tsc` included. Typecheck
baseline is **57 errors**; anything above that is mine.

---

## Done

### Storage — `internal/storage/research_delete_repo.go` (new)

- `ResearchRepository.DeleteCascade` — one transaction: collect session and
  roadmap ids (their counter keys are scoped by *their* ids), delete outgoing
  crossrefs, unresolve incoming ones, delete `storage_counters` rows, delete the
  research. Rows-affected decides, so two concurrent deletes do not both report
  success.
- `SectionRepository.DeleteCascade`, `SessionRepository.DeleteCascade`,
  `QuestionRepository.DeleteCascade` — same shape, each clearing the reference
  tables for what it takes with it.
- `ResearchRepository.DeletionSummary` — counts per table, plus incoming
  reference count and up to ten citing researches by code and name.

**The finding that shaped all of it: `crossrefs` has no foreign keys, in any
dialect.** Migration `007_crossref_nullable_source.sql` recreated the table to
make `source_entry_id` nullable and did not carry the `REFERENCES` clauses over;
the Postgres and MySQL baselines were generated from that shape, so all three
agree. Nothing about those rows is automatic. `external_links` has one on
`research_id`, which does not fire on a *section* delete because the research is
staying — same class of orphan. Both are handled explicitly, inside the delete's
own transaction.

`storage_counters` is also outside the FK graph, deliberately, so codes survive
deleting a document. Its keys are `table:prefix:scope`, matched with
`scope_key LIKE '%:' || id`.

### Service — `internal/service/research_delete.go` (new)

- `ResearchService.Delete` — `Get` first (so a stranger gets `ErrNotFound`
  before ownership is even consulted), then `Access.Admin`, then the member list
  read **before** the delete, then `DeleteCascade`, then the events.
- `ResearchService.DeletionSummary`, `SectionService.Delete(force)`,
  `SessionService.Delete`, `SessionService.DeleteQuestion`.
- `ErrSectionNotEmpty`, mapped to **409** in `handlers/errors.go`, message
  carrying the document count.
- `Access.Admin` added — CLAUDE.md describes Read/Write/Admin and only the first
  two existed.

**The second finding: `research.deleted` would have reached nobody.** The hub
decides delivery per event by asking `Access.CanReadResearch`, and after the
delete nobody qualifies because the research is gone. Two emissions, and they do
not overlap: one plain event (delivered when auth is off, where the hub lets
everything through and there is no user id to address) and one directed event
per team member via `TargetUserID` (delivered when auth is on, dropped when it
is off because there is no authenticated connection). The event also carries a
new `ResearchCode` field on `service.Event`, mapped through `ws/notifier.go`,
because the hub resolves the code by looking the research up — and it is gone.
`hub.go`'s existing `event.ResearchCode == ""` guard anticipated a caller
setting it.

### Tests — all green, `make test` passes whole

`internal/service/research_delete_test.go` — 8 tests: cascade counted per child
table (not trusting the FKs, per the acceptance criterion), counters reclaimed,
incoming references surviving as unresolved with `target_ref` intact, the
directed-event audience and the research code on it, owner-only refusal
(editor → `ErrForbidden`, stranger → `ErrNotFound`), the non-empty section
refusal naming the count, and a session keeping the documents written during it.

`internal/service/role_matrix_test.go` — the five new deletes added to the
viewer-refusal matrix. That matrix is where `roadmap remove nodes` was missing,
which is how the bug the audit found stayed invisible.

### API — `internal/api/handlers/delete.go` (new) + routes in `server.go`

`DELETE /api/researches/{id}`, `GET /api/researches/{id}/delete-preview`
(`accessRead`), `DELETE /api/sections/{sectionId}?force=`,
`DELETE /api/sessions/{id}`, `DELETE /api/questions/{questionId}`.

### MCP — 5 new tools (57 registered, was 52)

`research_delete` (requires `confirm`, returns what it destroyed),
`research_delete_preview`, `section_delete` (`force`), `session_delete`,
`question_delete`. Optional flags are `*bool` with a new `derefBool`, per the
repo's pointer rule.

### Frontend

`ModalOverlay` gained `initialFocus` (a child cannot win the focus race against
the parent's own watcher — `SendBackModal`'s textarea is broken the same way).
`components/research/DeleteResearchDialog.vue` (new) with the typed-code
confirmation. `composables/useResearchDelete.ts` (new) holding the failure
mapping. Danger zone on the settings Overview tab. `ResearchCard` swapped its
hover-revealed archive icon for an `ActionMenu` with Archive and Delete, and its
archive call finally catches its own errors. Projects list wired. Session delete
and question delete with `ConfirmModal`. `useAccessRevoked` gained a
`research_deleted` reason so a reader on a page inside a deleted project is told
what happened rather than that their access ended.

---

## Verified against a running binary

Seeded three projects on `:8111` (`.scratch/preview/`), drove the dialog in
headless Chrome, then checked the database through the API.

- Dialog: correct counts, the outside-consequences line ("1 share link stops
  working. 2 references from R2 stop resolving."), **focus lands in the code
  field** — which is what proves the `ModalOverlay.initialFocus` extension, since
  a child cannot win that race alone — Delete disabled before typing and on a
  wrong code, enabled by `  r1 ` (padded, lower case), then navigation to the
  list and the success toast.
- After the delete: R1 gone, R2 and R3 present, and R2's document still holds
  both references with `resolved: false`, null targets and its text unchanged.
- Section delete: `409 section is not empty: 1 document(s) would be deleted with
  it`; with `?force=true` it empties and leaves no orphaned `external_links`.
- MCP: `confirm: false` returns the teaching message and deletes nothing;
  `confirm: true` reports what it destroyed; `research_delete_preview` matches.

**One caveat on the confirm gate.** The SDK marks every property required, so an
*omitted* `confirm` fails schema validation with `missing properties:
["confirm"]` rather than reaching the validation message — and a model's obvious
repair to that is to add `confirm: true` reflexively. What actually carries the
rule is the field's schema description and the tool description, both of which
state it before the model fills anything in; the message fires on
`confirm: false`. Flagged to the usability reviewer rather than judged alone.

## Remaining

1. Fold in the fleet's findings — all eight dispatched, none skipped.
2. Open the PR.

Cut deliberately: **S4**, the `⋯` per section in the research sidebar. The
design specification names it as the first thing to cut and says to keep the
settings-page control instead, which is built.

---

## Decisions the PR body needs

- **200 `{"deleted": true}`, not 204.** The issue's API sketch says 204; every
  other delete in this codebase answers 200 with that envelope, and `sDeleted`
  exists for it. Consistency won; the sketch was illustrative.
- **No migration.** The cascades that exist are correct in all three dialects;
  what is missing has no foreign keys at all, so it is code rather than schema.
  030/004 stay with #116.
- **Section delete never sends `force` from the UI.** The refusal is the
  feature. The force path exists for the API and MCP, where the caller is
  explicit by construction.
- **Incoming references are not deleted.** They survive as unresolved text with
  `target_ref` intact. Deleting them would edit a research nobody asked to
  change and make its own history a lie about what it once cited.
- **Delete does not require archiving first** — the issue's open question,
  answered "no" there and followed here.
- **Export-before-delete is offered inside the dialog**, using the existing
  portable export; a failed download never blocks or resets the delete.
- **The 5xx copy promises atomicity** ("Nothing was changed") because the
  cascade is one transaction. If that ever stops being true, the sentence has to
  change with it.
