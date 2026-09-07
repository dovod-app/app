# Domain Guide

Complete reference for every entity in Dovod.

## Entities

### Research

Top-level container for an investigation project. Owned by a [team](#team) when authentication is enabled — your role in that team is what decides whether you may read it, write to it, or move it.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Project name |
| `goal` | string | Single declarative sentence — success criteria |
| `description` | string | What this research covers |
| `memory` | MemoryItem[] | Notes: id, text, created_at, author, version, optional session_id/session_code |
| `tags` | string[] | Categorization tags |
| `status` | enum | `active` / `completed` / `archived` |
| `code` | string | Auto-assigned: `R1`, `R2`... (global scope) |
| `team_id` | string | The owning team — the only thing permissions consult |
| `user_id` | string | Who created it. A record, never a permission |
| `team_name` | string | Read-only, resolved per request |
| `team_is_personal` | bool | Read-only: true when the owning team is a single user's own |
| `role` | enum | Read-only: **your** role on this research — `viewer` / `editor` / `owner` |
| `template_slug` | string | The [template](#template) this research was started from, or absent. Provenance — nothing reads it back to steer the work |
| `template_version` | int | Which version of that template was followed. Stamped because built-ins are refreshed from the binary at every boot, so the text behind a slug changes under an upgrade |

**Key rules:**
- One research per topic. Don't mix unrelated investigations.
- Research-specific methodology lives in private [skills](#skill). Legacy `instruction` text is migrated to an attached private skill marked `needs_trigger`.
- `memory` survives across sessions. Use `add_memory` plus the actual research `session_id` to append. `research_memory` edits/deletes individual IDs; edits require `version`. Legacy notes have unknown authors and null creation dates. REST: GET/POST `/api/researches/{id}/memory`, PATCH/DELETE `/api/researches/{id}/memory/{itemId}`, POST `/api/researches/{id}/memory/bulk-delete` with explicit `ids`.
- `goal` is a success criterion, not a question. "Identify top 3 competitive threats" not "What are the threats?"
- A new research lands in the creator's personal team unless another is named: `research_create` and `research_import` take an optional `team_id`, as do `POST /api/researches` and `POST /api/researches/import?team=`.
- `template_slug` and `template_version` are written once, by `research_create` with a `template_slug`, and never again — no tool or route updates them, and `POST /api/researches` has no such field. They record what was followed, not what governs: the methodology text lives in the [template](#template) and the how-to-work text in the [skills](#skill) it attached.
- `team_name`, `team_is_personal` and `role` are computed per request and returned by `research_get`, `GET /api/researches/{id}` and `GET /api/researches`. They are not stored and are ignored on input.
- Moving a research between teams is `POST /api/researches/{id}/transfer` — there is no MCP tool for it, and it is the only way a research changes audience.

**The continuation summary.** `research_resume` and `GET /api/researches/{id}/resume`
answer one question — a new chat has opened on this research, what is unfinished?
It is a **projection**: nothing here is stored, and reading it creates no session,
moves no status and marks no document as seen.

```
GET /api/researches/{id}/resume?session_id=SS2&limit=5
```

`{id}` is a UUID or an `R` code; `session_id` is a UUID or an `SS` code resolved
inside that research, and one from another research answers `not found`. `limit`
is per group, 1–15, default 5 — out of range is clamped, and over REST a
non-numeric `limit` is `400` rather than a silent default. A `viewer` may read
it; `research.can_write` on the response says whether the actions it proposes are
ones you may actually take.

The payload is `schema_version`, `generated_at`, `research`, `sessions`,
`work.{in_progress,blocked,pending}`, `questions.{open,deferred}`,
`annotations.{to_work,awaiting_human}`, `recent_entries`, `next_actions` and
`truncated` / `note`.

- **Every group is `{items, returned, total, has_more, more}`.** `total` is
  nullable so "we could not count" is distinguishable from zero, and `more`
  names the tool and the page that open the rest. A failed read is an error, never
  an empty queue — an agent that reads "0 tasks" from an outage concludes the work
  is done.
- **It never chooses between two open sessions.** `SessionRepository.FindActive`
  is a `LIMIT 1` with no `ORDER BY`, so the `active_session` on `research_get` is
  an arbitrary pick when several are active. This returns them all with
  `selection_required: true`, no `selected_id`, and empty question groups. With
  none active it shows the most recently created session with its real status.
- **The annotation split is the contract, not a convenience.** `to_work` is
  `open` — work an agent may do. `awaiting_human` is `answered` — waiting for the
  person who raised the objection to accept it, which no amount of agent effort
  moves. See [Annotation](#annotation).
- **`recent_entries` is not a change log.** It is the newest `updated_at` first,
  its `total` is how many documents the research has rather than how many changed,
  and a deleted document leaves no trace. Each row carries `author_kind` and
  `revision` of the newest [revision](#revision): `human` there is a person's
  correction made in the web UI, and the reason to read before rewriting. Both
  are **absent** for a document written before revisions existed, and absent
  again if the revision lookup itself fails — that one failure degrades the two
  numbers rather than the list. Absent means unknown; it does not mean `agent`.
- **`next_actions` is at most three**, in a fixed order — resolve session
  ambiguity, continue an `in_progress` task, settle an open mark, start the
  highest-priority pending task, take the next question — each with `kind`,
  `target` (type, id, code, and the `session_code` / `entry_code` a URL needs),
  a closed-vocabulary `reason_code`, a sentence, and `actor`. `actor: "human"`
  means a person must act; it is not a statement about importance.
- **The size cap is 24 KiB.** Over it, previews are cut first, then items, and
  `truncated` plus a one-line `note` say so. Totals, `has_more` and the `more`
  links survive every step, and the JSON is valid at each — a row that did not
  fit is not a row that does not exist. Text is cut by rune, never by byte.
- **It is not the personal document queue** described under [Entry](#entry):
  that one is per reader and is acknowledged by writing to it, this one is the
  same for every reader of the research and acknowledges nothing.

---

### Team

Who may do what. A team owns researches; membership in that team is the whole of the access model, and every user gets a personal team when they register — a solo user never has to think about any of this.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The creator's own name for a personal team, otherwise whatever was typed |
| `personal` | bool | Created alongside its user. Cannot be renamed away from its owner, deleted, or left |
| `role` | enum | Read-only: the caller's role in this team |
| `member_count` | int | Read-only summary |
| `research_count` | int | Read-only summary |

**Roles** (`viewer` → `editor` → `owner`, each adding to the one before):

| Role | May |
|------|-----|
| `viewer` | Read everything in the team's researches — entries, sessions, questions, tasks, roadmaps, revisions — and export them |
| `editor` | Everything above, plus create and change entries, sections, sessions, questions, tasks and roadmaps |
| `owner` | Everything above, plus manage members and invitations, rename or delete the team, and transfer a research into or out of it |

**What a failure looks like:**

- **Not a member of the owning team** → the research does not exist for you: `not found` from a tool, `404` from REST. Confirming that someone else's research exists is itself a leak, so this is deliberate.
- **A member without the right** — a `viewer` writing — → `your role in this team does not allow this`, `403` from REST. Hiding a research from someone who can already read it would protect nothing.
- With `auth_enabled: false` there is no caller and no check: everything lives in one local team and every operation is permitted, exactly as before teams existed.
- Roles are the second gate, not the first. Before any of them, the request has to be taken at all: with accounts on, every route but the public handful needs `Authorization: Bearer <JWT | API key | OAuth access token>`; with accounts off but an `api_token` configured, every **write** and every MCP call on either socket transport — Streamable HTTP at `/mcp` and the legacy SSE listener — needs `Authorization: Bearer <api_token>` as a header on every transport — the instance token is refused in a `?token=` query string, which is for account tokens only; with neither configured, those same requests are accepted only from a client on the machine the server runs on — a loopback peer address, carrying no `X-Forwarded-For`, `X-Real-IP` or `Forwarded` header, naming a loopback `Host`, and with any `Origin` also loopback — and answer `401 the write API is disabled: set api_token or auth_enabled to accept writes from another machine` to anyone else. REST **reads** ask for nothing in both credential-less postures. Full table: [MCP Client Guide → Connecting Over HTTP](/llms/mcp-client-guide.md#connecting-over-http-which-credential-this-instance-wants).
- The WebSocket at `/ws` needs the same credential as everything else when auth is on, and delivery is decided **per event, per connection**: a research event reaches only those who may read it, a team event only its members, and losing access stops the updates on a socket already open. See [Real-time Events](#real-time-events) below.
- That local team survives if auth is later turned on. It has no members, so its researches are **readable by every signed-in user and writable by none** until the first registration claims them — a deliberate compromise between stranding them behind a team nobody can join and letting the first caller take them for themselves.

**A [share link](#share) is the one way to read a research without a role.** It grants no membership and names no person: it is a capability over one research, resolved to `viewer` on that research and nothing else, and every write made through one is refused before any role is consulted.

**Cross-references resolve for the reader, not the author.** `[[R2:E5]]` is stored resolved whatever the writer may see, and every read path strips the targets its reader cannot follow — so a reference into a colleague's research works for whoever is entitled to it, and reads back as plain text for whoever is not.

**Invitations are links, never emails.** The server sends nothing; an owner creates an invitation and hands the link over themselves.

- `POST /api/teams/{id}/invites` returns the token **once** — it is stored hashed and cannot be read back.
- The invitation carries the role it was created with and expires after 14 days.
- `GET /api/invites/{token}` previews it without consuming it and works without a session (`status`: `pending` / `expired` / `revoked` / `accepted` / `unknown`); signed in, it also reports whether you are already a member and whether the address matches.
- `POST /api/invites/{token}/accept` joins the signed-in user. An address mismatch is allowed — the token is the capability — and accepting a second time reports that you are already a member.
- `DELETE /api/invites/{id}` revokes an unused one. A revoked link then previews as `revoked` and nothing else — no team, no sender — because withdrawing an invitation that went to the wrong person should withdraw what it disclosed.
- A personal team accepts no invitations: it refuses every removal, so a member could never be taken out again.
- `POST /api/auth/register` accepts an `invite_token`. A valid one gets the account created **even where `allow_registration` is false** and joins the team in the same request: the invitation is the authorization. It follows that a leaked link is a way onto a closed server, for one person, until it is used or expires.

**REST routes** (also in the [OpenAPI spec](/api/openapi.yaml), which is generated from the router; the table below adds which role each one needs):

| Method | Path | Needs |
|--------|------|-------|
| `GET` | `/api/teams` | a signed-in user — returns your teams with your role and counts |
| `GET` | `/api/teams/{id}` | membership |
| `POST` | `/api/teams` | a signed-in user |
| `PUT` | `/api/teams/{id}` | owner; refused on a personal team |
| `DELETE` | `/api/teams/{id}` | owner; refused while it still holds researches, and on a personal team |
| `GET` | `/api/teams/{id}/members` | membership |
| `PUT` | `/api/teams/{id}/members/{userId}` | owner; cannot demote the last owner |
| `DELETE` | `/api/teams/{id}/members/{userId}` | owner, or yourself to leave; cannot remove the last owner |
| `GET` | `/api/teams/{id}/invites` | owner — open invitations, expired ones included |
| `POST` | `/api/teams/{id}/invites` | owner — returns the link once |
| `DELETE` | `/api/invites/{id}` | owner of that team |
| `GET` | `/api/invites/{token}` | nobody — public preview |
| `POST` | `/api/invites/{token}/accept` | a signed-in user |
| `POST` | `/api/researches/{id}/transfer` | owner of the source team, write access in the target |

Every `/api/teams` and `/api/invites` route except the public preview needs a session: with `auth_enabled: false` they answer `401 sign in to manage teams`, because there are no users to put in a team. The transfer route is the exception — with no caller there is nothing to check, and it moves the research, for a caller the server takes a write from at all (with no `api_token` either, one on its own machine).

`GET /api/researches` lists every research across all your teams and takes `?team={id}` to narrow it to one, and each item carries `team_id`, `team_name`, `team_is_personal` and `role`.

---

### Share

A revocable, read-only capability over **one** research, addressed by an unguessable token rather than by who is holding it. Not an account, not an invitation, not a role: nobody signs in, the link names no user, and a visitor holding one is never a member of anything.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Identifies the link — for the manage list and for revocation. Not the token |
| `research_id` | string | The single research it reaches. Everything else is out of scope by construction |
| `scope` | enum | `research` — the only value issued. `session` / `entry` / `roadmap` exist in the schema for narrower forms and are refused today |
| `label` | string | Free text for the owner: "Client review, March" |
| `include` | object | `sessions`, `tasks`, `roadmaps`, `export` — four booleans, described below |
| `has_password` | bool | Whether a password is set. The hash is returned to nobody |
| `expires_at` | string | `null` for a link that never expires on its own |
| `revoked_at` | string | Once set, the link answers exactly as if it never existed |
| `last_seen_at` | string | When it was last opened |
| `view_count` | int | Counted once per page load, not once per fetch |
| `created_by_name` | string | Read-only, resolved per request — who handed the link out |
| `research_code` | string | Read-only, so a client can build the URL it already knows |

**No short code.** A share is never referenced from content, so there is no `[[…]]` form for one; it is addressed by its token and by `id`.

**The token** is 256 random bits behind an `mrs_` prefix, stored as SHA-256 alongside API keys and team invites. It appears in exactly one response — the one that created it — and no route can read it back. A leaked database hands out no working links.

**Include flags.** The zero value is the safe one: a flag has to be set to reveal something, so nothing leaks by being forgotten. `roadmaps` defaults to true at creation because roadmaps are part of the research proper; `sessions`, `tasks` and `export` default to false, being working state rather than a result.

**Owner routes.** Creating, listing, changing and revoking all need **write** access — an editor or an owner. A viewer cannot republish what the team owns; an editor could already export the whole research to a file and send it, so a link is not a capability they lacked.

| Method | Path | Returns |
|--------|------|---------|
| `POST` | `/api/researches/{id}/shares` | `201` with `share`, `token` (the only time it exists) and `url` — the page a visitor opens, `https://<host>/s/<token>` |
| `GET` | `/api/researches/{id}/shares` | every share of that research, live and dead, metadata only |
| `PUT` | `/api/shares/{id}` | changes a live link's `label` and `include` in place — same token, same address |
| `DELETE` | `/api/shares/{id}` | revokes it |

The create body takes `label`, `include` (any subset of the four booleans), `expires_in_days` (omit for a link that never expires; clamped to 1–3650) and `password` (optional, at least 6 characters). `GET /api/researches/{id}` carries `active_share_count` beside the research — `0` for anyone who could not manage the links anyway, a share visitor reading through one of them included.

**Changing a live link.** `PUT /api/shares/{id}` takes `label`, `include`, or both, and answers `200` with the whole share in the shape the list returns — never the token, which no route can read back. The address is what survives, and that is the point: a published link is one people have already saved, so revoking it in order to show something more breaks every place it was pasted. **`include` is a complete replacement, not a patch** — send all four booleans. A flag left out of the object is `false`, so a body naming one flag takes the other three away; omit `include` entirely to leave the flags alone, and omit `label` to leave the label. A body with neither is `400`. Widening reaches everyone already holding the URL at once, on their next request. A revoked or expired link answers `409` — this is the owner's own management surface, where "the link died while you had this open" is the useful answer, unlike the visitor prefix where every refusal must look the same. An unknown id, or one belonging to another team, is `404`. Neither the token, the password nor the expiry can be changed: those need a new link.

**Visitor routes** live under their own prefix, `/api/shared/{token}/…`, behind their own middleware. The token is checked once, at the prefix; there is no path by which it reaches a route built for an owner. Inside the prefix the paths are the ones the authenticated API uses, so `/api/shared/{token}/researches/R1/entries` is served by the ordinary entries handler.

| Under `/api/shared/{token}` | Method | Needs |
|---|---|---|
| (the prefix itself) | `GET` | — the share payload: what this link is, the research, its sections |
| `/unlock` | `POST` | — exchanges a password for the unlock value |
| `/researches/{id}`, `/researches/{id}/entries`, `/researches/{id}/sections/{sectionId}/entries`, `/researches/{id}/tags` | `GET` | — |
| `/researches/{id}/entries/{entryId}`, `/researches/{id}/entries/by-code/{code}`, `/entries/{id}`, `/entries/{id}/related` | `GET` | — |
| `/researches/{id}/crossrefs`, `/entries/{id}/crossrefs`, `/researches/{id}/links`, `/entries/{id}/links` | `GET` | — |
| `/researches/{id}/graph` | `GET` | — the route always answers; the nodes the flags do not cover are left out of it (below) |
| `/researches/{id}/roadmaps`, `/researches/{id}/roadmaps/{roadmapId}`, `/roadmaps/{id}` | `GET` | `include.roadmaps` |
| `/researches/{id}/sessions`, `/researches/{id}/sessions/{sessionId}` | `GET` | `include.sessions` |
| `/researches/{id}/tasks` | `GET` | `include.tasks` |
| `/researches/{id}/export` | `GET` | `include.export` |

That list is the whole surface. Anything else under the prefix — another method, another path, a route whose flag is off — answers the same `404 this link is no longer available` that a revoked, expired or invented token gets. A link without sessions is meant to look like a research that has none.

**The graph gates itself, inside the handler.** `…/researches/{id}/graph` is mounted ungated because sections and documents are always in a link; what a flag decides is which nodes it draws. Session, question and task nodes appear only when `include.sessions` / `include.tasks` allow them, and nothing below the handler would have refused them — a share resolves to `viewer` on this very research, and this is the one route that assembles every entity into a single payload. The body carries `available_node_types` beside `nodes` and `edges`: the types this caller *could* have received — `section` and `entry` always, `session` and `question` with sessions, `task` with tasks — so a client builds its filter list from that rather than repeating the mapping. It never names what was withheld: a type the flags do not cover is absent from `nodes` and from `available_node_types` alike, rather than present and empty. The same field is on the authenticated route, where it lists every type. The visitor's own pages sit under the link's address: `/s/<token>` for the research, `/s/<token>/graph` and `/s/<token>/mindmap` for the two views.

**What a visitor never sees:**

- Private skills and memory — memory is redacted on research reads, and skill services and exports exclude skills for share contexts. They are the agent's working notes about how to conduct the research, not a result, and their author did not publish them by sending a link to the findings.
- `user_id`, `team_id`, `team_name`, `team_is_personal` — a share is about one research, not about the organisation behind it. `role` survives and is always `viewer`.
- The section's `instruction`. How a team writes here is working process, like the memory above it: a visitor was handed the findings, not the conventions they were written under. It is stripped from every section a share reads, so the shared research page, the graph, the export and the vault never carry one.
- Document metadata, values and declaration both. An entry's `metadata`, `spec_version` and `metadata_status` are stripped, and so is the section's `field_spec` — a list of twelve field labels with nothing in them still says what the team decided to track, and the values are exactly the facts a declaration invites a team to record: an owner, a cost, an interviewee, an internal ticket.
- Any other research. There is no list route under the prefix, and the listing service itself answers empty for a share rather than falling through to "no user in context, so no filter" — which would have returned every research on the server.
- The portable JSON: its route is not mounted. The Obsidian vault is available when `include.export` allows it, with memory and private skills excluded; revisions and provenance are refused. See [Export](/llms/export.md#export-through-a-share-link).
- Any [template](#template) — no list, no body, and no stamp. `TemplateService` refuses a share context before it resolves anything, and `template_slug` / `template_version` are blanked on the research alongside memory: a slug is a name a team chose, and it would read back as that name.
- Revision history and search — neither route exists under the prefix. The knowledge graph does, with the node types the flags do not cover withheld inside the handler as described above. The mindmap is not a route in either direction: it is assembled in the browser from the research, its sections and documents, its sessions, tasks and cross-references, so a link narrows the mind map by narrowing those.
- The continuation summary. `/api/researches/{id}/resume` is not mounted under the prefix, and `ResumeService` refuses a share context before it resolves anything — ahead of `Access.Read`, which would allow it, since a share does resolve to `viewer` on this very research. What is unfinished, what a person disputed and what the agent should do next is working process, like private skills.
- Every write, without exception. `Access.Write` refuses a share context before it looks at any role, so this does not depend on the `viewer` it resolves to.

**Cross-references** out of the shared research (`[[R2:E5]]`) render as inert text — not a link, not a 404, because a share must not confirm that R2 exists. An *incoming* reference from a research the visitor cannot open is dropped from the list rather than blanked: the stripped row would still announce that something unseen cites this entry.

**Password.** Optional, bcrypt. `POST /api/shared/{token}/unlock` returns an `unlock` value, sent back on every later request as `X-Share-Unlock` — or as `?unlock=…` where a header cannot be set, which is the WebSocket and a download the browser navigates to. A locked link answers `401` with `reason: password_required`; a wrong password answers `401` with `reason: invalid_password`, the one place the distinction is made, since whoever is standing at the prompt already knows the link is real. The unlock value is derived from the share, not stored: it dies with the link and stops working the moment the password changes. It is not a session, and there is nobody to have one.

**Rate limits.** Reads under the prefix: 600 per minute per address — one page load is many fetches. Unlock: 10 per minute counted against the link itself as well as the address, because spreading guesses across addresses must not spread the budget. Both answer `429` with `Retry-After: 60`.

**Revocation** takes effect on the next request; every layer consults the share per request. The exception is an open WebSocket, which re-resolves the link on its own timer and closes within the minute — see [Real-time Events](#real-time-events).

**No MCP tool creates, lists or revokes a share.** Handing out a public link is a human act; the tool list is unchanged. A share token is a REST credential only — it never reaches an MCP endpoint. Shares also work with `auth_enabled: false`, where the row simply records no creator — but issuing one is a write, so on an instance with no `api_token` either it can only be done from the machine the server runs on. Visiting a link is not: the routes under `/api/shared/{token}/…` take the token as their whole credential and are reachable from anywhere, whatever posture the instance is in.

---

### Section

Logical division within a research. Organizes entries by topic.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Slug identifier (e.g., `market-analysis`) |
| `display_name` | string | Human-readable name |
| `description` | string | One sentence scope definition |
| `position` | int | Sort order (0-based) — investigation sequence |
| `status` | enum | `draft` / `active` / `completed` / `archived` |
| `code` | string | Auto-assigned: `S1`, `S2`... (per research) |
| `instruction` | string | How to write a document **in this section** — three to six lines of imperatives, read before every document filed here. At most 500 characters, counted in runes. Empty is the normal case |
| `field_spec` | object[] | What documents in this section record: `{key, label, type, required, repeated, options, help}`. Empty is the normal case and means the section accepts no metadata at all |
| `spec_version` | int | Bumped when `field_spec` actually changes; entries record the version their values were validated against |

**Key rules:**
- 3-7 sections per research. Non-overlapping scopes.
- Order by investigation logic (context → current state → gaps → recommendations), not alphabetically.
- Requires at least one entry before marking `completed`.
- A section is usually a *topic* and declares nothing. Declare `field_spec` only when it holds one class of document repeatedly — eighteen specifications, not eight loose questions. The vocabulary is then closed: an entry may write those keys and no others. See [Document Metadata](/llms/metadata.md).
- `field_spec` is settable only through `section_update` / `PUT /api/sections/{sectionId}` — neither `research_create` nor `research_add_section` accepts one. A portable import is the exception: the declaration travels with the section.
- `instruction` is settable the same two ways and nowhere else, with the same import exception — where an over-long one is dropped rather than refused, and named in the import's `warnings` so the loss is visible. `null` leaves it alone and `""` removes it; over REST the property may also simply be left out, while the `section_update` schema requires it like every other property, so send `null` there. Over 500 runes it is **refused**, never truncated — `instruction must be 500 characters or fewer…`, a `400` over REST — because half a rule reads like a whole one.
- **An instruction says what a document here looks like and nothing wider.** The research's memory says what *this research* is, a [skill](#skill) says how a *kind of work* is done, and this says how to write in this section; most specific wins on a direct conflict. An instruction restating research-wide tone or methodology is misfiled. Where the section also declares `field_spec`, the instruction should name those keys, or the two end up describing the same document differently. See [Skills → Three places a rule can live](/llms/skills.md) and [Document Metadata](/llms/metadata.md).
- `section_list` and `research_get` return `spec_version` on every section, and `field_spec` and `instruction` only when they are non-empty. REST section payloads carry `instruction` always, as `""` when there is none.

---

### Entry

A document containing research findings. Lives in a section. Holds either markdown
or, when `entry_type` is `blocks`, a JSON document of typed blocks — prose next to
tables, callouts, checklists, mermaid diagrams, projected tasks, conversation
transcripts and self-contained HTML rendered in a sandboxed iframe. See [Block Documents](/llms/blocks.md). `entry_type: artifact`
is an input alias for a block document holding one `html` block; it is never
stored, and such an entry reads back as `blocks`. A `code`, `mermaid` or `html`
body is not searched as text and contributes no cross-references.

| Field | Type | Description |
|-------|------|-------------|
| `entry_type` | enum | `markdown` (default) / `blocks` (`artifact` accepted on input, stored as `blocks`) |
| `title` | string | Entry title (auto-generated from content if omitted; for a block document from its first heading, and the call fails when there is nothing to take one from) |
| `content` | string | Markdown, a block document, or a full HTML document for the `artifact` alias |
| `description` | string | Short summary (auto-generated if omitted) |
| `section_id` | string | Parent section |
| `session_id` | string | Optional: session that produced this entry |
| `tags` | string[] | Categorization tags for filtering — free-form and cross-cutting, and not a substitute for `metadata` |
| `metadata` | object | Values for the fields this entry's section declares, keyed by field key. Absent when the section declares nothing |
| `spec_version` | int | The section spec version these values were last validated against |
| `metadata_status` | object | Computed on every read, never stored: `{complete, missing_required, orphaned, issues, spec_version}` |
| `status` | enum | `draft` / `active` / `completed` / `archived` |
| `code` | string | Auto-assigned: `E1`, `E2`... (per research) |

**Key rules:**
- One entry per topic. Synthesize multiple answers into one coherent document.
- Use `[[E3]]` syntax in content to cross-reference other entries.
- Tags enable filtering on the research page. Use consistent taxonomy.
- `metadata` only accepts keys the section declares; anything else is dropped and named in `metadata_report` on the response. No metadata problem ever fails a write — the single exception is moving to `status: completed` with required fields unanswered, which is refused (`409 metadata_incomplete` over REST) unless `allow_incomplete` is set. A value of `null` is an explicit unknown and answers a required field, which is what you send instead of guessing. `entry_read` returns the values; `entry_list` does not. See [Document Metadata](/llms/metadata.md).
- Title/description auto-generated from content if not provided.
- `session_id` tracks provenance. When you leave it empty, the server links the entry to the research's active session automatically; the session export (`/research/{code}/session/{sessionCode}/export`) lists entries by this link.
- Every write that changes an entry appends a [revision](#revision); `entry_history` says who wrote each one and `entry_diff` what it changed. Read them before rewriting an entry a previous session produced.
- Deleting an entry (`entry_delete`) also deletes its cross-references, its extracted external links, its whole revision history, and every [annotation](#annotation) anchored in it.
- A person may mark a sentence in an entry. Those marks anchor to a block id and a quote, survive a rewrite where they can, and an entry write reports the ones it drifted or orphaned — see [Annotation](#annotation).
- A blocks entry is edited whole with `entry_update` or block by block with `entry_patch`; `text_replace` is refused on it.
- URLs found in entry content are extracted into an external-links index, readable at `GET /api/entries/{id}/links` and `GET /api/researches/{id}/links`.
- One document can be taken out on its own: `GET /api/entries/{id}/markdown` returns it as a `.md` file with YAML front matter — the vault's, minus `aliases` and `session` — named `E50 — Title.md`. The `{id}` is the entry UUID, not an `E`-code. There is no MCP tool for it (`entry_read` already gives an agent the content), the file leaves `[[E3]]` exactly as stored, and the route is not on the share sub-mux. See [Export](/llms/export.md).
- One document can also be put in on its own: `POST /api/sections/{id}/import/preview` parses an uploaded `.md` file and writes nothing, `POST /api/sections/{id}/import` commits the accepted fields as one entry. `{id}` is the section UUID. Both need `editor` or `owner`, the preview included; neither is on the share sub-mux. The file always lands as a `markdown` entry with a new `E`-code and revision 1 authored `import`; title, description, status, tags and declared metadata survive the round trip from the download, codes and cross-references deliberately do not. There is no MCP tool — an agent writes with `entry_create`. See [Export → One File into a Section](/llms/export.md).

**Personal document updates (REST and web UI only).** A reader has one
server-side checkpoint per entry: the numbered revision they actually saw. It
belongs to the reader, not to the team document. With authentication it follows
the user between devices and one member cannot clear another member's queue;
with authentication off the installation has one `local` reader.

An ordinary REST document read carries this object at the **response root**, not
inside `data`, captured from the same database snapshot as the root `revision`:

```json
{
  "view_state": {
    "current_revision": 7,
    "seen_revision": 4,
    "unseen_revisions": 3,
    "kind": "changed"
  }
}
```

No checkpoint is `seen_revision: 0`, `kind: "new"`; one behind the current
revision is `changed`; one at the current revision is `seen`. The GET itself
does not mark anything. The web client calls the seen route only after it has
rendered the document, and sends the exact `current_revision` it rendered — the
server never substitutes whatever is newest by the time that write arrives.

```
GET  /api/researches/{id}/updates       this reader's new and changed documents
PUT  /api/entries/{id}/seen             {"revision": 7}
POST /api/researches/{id}/updates/seen  {"entries": [{"entry_id": "…", "revision": 7}]}
```

The queue response is `data: {entries, new, changed, count}`. A row has no
content; it carries the entry and section ids, entry code, title, description,
type, status, `current_revision`, `seen_revision`, `unseen_revisions`, `kind`
and the current revision's `updated_at`. The single-entry route takes an entry
UUID. The bulk route accepts at most 2000 exact entry/revision pairs and is
atomic: one pair outside that research or one nonexistent revision rejects the
whole operation. Checkpoints only advance, so a late request from an older tab
cannot make a document unread again. More importantly, a revision committed
after the page took its bulk snapshot remains in the queue.

A `viewer` may read and acknowledge this state; acknowledging is not an edit to
the shared document. There is no MCP tool, no export field and no share route,
and a share document response carries no `view_state`. An MCP client that needs
to understand a change uses `entry_history` / `entry_diff` and never consumes a
person's queue.

**Cross-reference syntax in content:**
- `[[E3]]` — entry E3 in same research
- `[[R2:E5]]` — entry E5 in research R2
- `[[R2]]` — link to research R2 itself

---

### Session

Interactive Q&A interview workflow. How knowledge enters the system.

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Session title |
| `focus` | string | Specific topic for this session |
| `notes` | string | Accumulated observations and pivots |
| `status` | enum | `active` / `completed` / `archived` |
| `code` | string | Auto-assigned: `SS1`, `SS2`... (per research) |

**Key rules:**
- **One active session at a time** per research. Complete before starting another.
- **Multiple sessions are normal** — each focuses on different aspects (initial exploration, deep-dive, follow-up).
- Use `add_note` to log decisions and pivots during the session. Notes support markdown and `[[...]]` cross-references.
- Create entries when enough material accumulates from Q&A. Set `session_id` on entries to track provenance.

**Typical session progression:**
1. Create session with 3-8 initial questions
2. Work through questions one at a time
3. Record answers, create follow-ups as needed
4. When enough answers accumulate → create entry in appropriate section
5. Mark session `completed` when all questions addressed

---

### Question

Structured Q&A prompt within a session.

| Field | Type | Description |
|-------|------|-------------|
| `text` | string | The question |
| `area` | string | Topic area (matches section topics) |
| `rationale` | string | Why this question matters |
| `priority` | enum | `high` / `medium` / `low` |
| `status` | enum | `pending` / `in_progress` / `answered` / `deferred` / `skipped` |
| `answer` | string | Captured response (required when `answered`) |
| `parent_id` | string | Optional: parent question for follow-ups |
| `code` | string | Auto-assigned: `Q1`, `Q2`... (per session) |

**Statuses:**
- `pending` — not yet asked
- `in_progress` — currently being discussed
- `answered` — answer recorded (non-empty `answer` required)
- `deferred` — postponed, still valid
- `skipped` — deliberately skipped, no longer relevant

**Key rules:**
- Nesting max 3 levels deep (parent → child → grandchild).
- One topic per question. Don't combine multiple asks.
- `area` enables filtering by section focus.
- Question text, answers, and rationale all support cross-references (`[[E3]]`).
- Answers support full markdown formatting.

---

### Task

Todo item for tracking work within a research.

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | What needs to be done |
| `description` | string | Task details |
| `priority` | enum | `high` / `medium` / `low` |
| `status` | enum | `pending` / `in_progress` / `blocked` / `completed` / `failed` / `deferred` |
| `result` | string | Outcome (set when completing) |
| `code` | string | Auto-assigned: `T1`, `T2`... (per research) |

**Tasks vs Questions:** Questions capture knowledge through interview ("What is X?"). Tasks track work items ("Do X"). Use questions for knowledge extraction, tasks for action tracking.

**Key rules:**
- Always fill `result` when completing a task.
- Results support markdown and cross-references.
- Use `blocked` status with reason in description when waiting on something.
- A task can be projected into a `blocks` entry with a `task_ref` block, which
  stores the reference and reads the title and status from here. A tick in the
  document is a `status` change on the task, so there is still one place the state
  lives. See [Block Documents](/llms/blocks.md).

---

### Roadmap

Visual graph for learning paths, strategy maps, decision trees, or step-by-step guides. Unlike the auto-generated mindmap, roadmaps are deliberately designed by the AI or user.

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Roadmap title |
| `description` | string | What this roadmap visualizes |
| `statuses` | string[] | Available node statuses in order (e.g. `["not_started", "in_progress", "completed"]`) |
| `status` | enum | `active` / `completed` / `archived` |
| `stages` | string[] | Ordered stage/column names for the **stages** view. Empty by default. A name with no nodes is an empty column; relates to a node's `stage` as `statuses` relates to a node's `status` |
| `view` | enum | `graph` (default) / `stages` / `timeline` — the layout the roadmap opens in. The UI toggle overrides it locally |
| `code` | string | Auto-assigned: `RM1`, `RM2`... (per research) |

**Key rules:**
- A research can have multiple roadmaps for different aspects.
- `statuses` defines the vocabulary for node progress. Choose statuses that fit the domain:
  - Learning path: `not_started`, `learning`, `practiced`, `mastered`
  - Marketing strategy: `planned`, `approved`, `launched`
  - Technical plan: `todo`, `in_progress`, `review`, `done`
- Leave `statuses` empty for purely structural graphs with no progress tracking.
- Create the entire graph in one `roadmap_create` call when possible — nodes and edges together.

**When to create a roadmap:**
- The research topic has a natural sequence or progression (learning path, onboarding flow, migration plan)
- You need to visualize dependencies or decision points (architecture decisions, strategy branches)
- The user wants a step-by-step guide with trackable progress
- Information needs spatial/hierarchical organization beyond flat sections

**When NOT to use a roadmap (use sections/entries instead):**
- Content is purely textual with no inherent sequence
- A simple list or table would suffice
- The information doesn't have meaningful relationships between items

---

### Roadmap Node

A node in a roadmap graph. Represents a step, milestone, decision point, or informational block.

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Node title |
| `description` | string | Detailed text content (expandable in UI) |
| `node_type` | string | One of the nine types below (default `step`) |
| `status` | string | Current status (from parent roadmap's `statuses` list, or empty) |
| `stage` | string | Which **stages**-view column the node sits in. Matched against the roadmap's `stages`; empty or unknown falls into a trailing Unassigned column |
| `node_date` | string | ISO `YYYY-MM-DD` (or empty) placing the node on the **timeline** — its point, or a range **start**. A dated `milestone` is a diamond on the axis |
| `node_end_date` | string | Optional ISO `YYYY-MM-DD`. With `node_date`, the node is a **bar** from start to end; empty is a point. `400` if before the start; ignored for a milestone |
| `position_x` | float | X position for layout |
| `position_y` | float | Y position for layout |
| `parent_id` | string | Optional: parent node for hierarchical nesting |
| `ref_type` | string | Optional: `entry` / `task` / `session` / `research` / `question` |
| `ref_id` | string | Optional: ID of the referenced entity |
| `ref_data` | object | Read-only: referenced entity resolved at read time, never stored |
| `metadata` | string | Optional: JSON blob for node-type-specific data (checklist items, URL, metric value) |
| `code` | string | Auto-assigned: `N1`, `N2`... (per roadmap) |

**Node types:**
- `step` — Regular action item or learning step (default)
- `milestone` — Key achievement or checkpoint
- `decision` — Fork in the path where a choice is needed
- `info` — Reference material or prerequisite note
- `group` — Container for related steps (visual grouping)
- `checklist` — Sub-items with checkboxes (`metadata` holds the items)
- `note` — Free-form sticky-note annotation
- `link` — External URL reference (`metadata` holds the URL)
- `metric` — KPI or numeric indicator (`metadata` holds the value)

**Key rules:**
- Use `temp_id` during creation to reference nodes in edges before they have real IDs.
- Status is free-form but should match the roadmap's `statuses` list for consistent UI rendering.
- Position coordinates are optional — the frontend can auto-layout, but explicit positions give more control.

---

### Roadmap Edge

A directed connection between two roadmap nodes.

| Field | Type | Description |
|-------|------|-------------|
| `source_node_id` | string | Source node UUID (the create tools take it as `source`, accepting a `temp_id`) |
| `target_node_id` | string | Target node UUID (the create tools take it as `target`, accepting a `temp_id`) |
| `label` | string | Edge label (e.g. "next", "if yes", "alternative") |
| `edge_type` | string | `default` / `success` / `warning` / `optional` |

**Edge types:**
- `default` — Normal progression
- `success` — Positive outcome path (e.g. "if passed")
- `warning` — Failure or risk path (e.g. "if blocked")
- `optional` — Non-required alternative path

**Key rules:**
- Edges are deleted automatically when their source or target node is removed (cascade).
- Use labels to clarify the relationship, especially for `decision` nodes with multiple outgoing edges.

---

### Revision

Every write that changes an entry appends a snapshot: content, title,
description, `entry_type`, status and tags as they stood after that write, plus
who wrote it (`agent`, `human`, `import`, `restore`), the session that was active
at the time, and a short summary of what changed.

| Field | Type | Description |
|-------|------|-------------|
| `revision` | int | 1-based per entry, never reused. A number, not a short code |
| `author_kind` | enum | `agent` / `human` / `import` / `restore` |
| `session_id` | string | Session **active when the write happened** — not the entry's own `session_id` |
| `summary` | string | "Updated content, tags", "Patched blocks: inserted 2", "Restored revision 1" |
| `content` | string | The entry's body at that point. Omitted from list responses, present when one revision is read |

**Not created by:** a write that changes nothing, or a checkbox tick — ticking a
box is not an edit to the document.

**Restoring** an earlier revision writes a new one holding its content, so
history is append-only and a restore is itself undoable. Checkbox state and
block ids survive a restore.

Read with `entry_history` and `entry_diff`; `GET /api/sessions/{id}/changes`
rolls it up per session. Not to be confused with a blocks entry's `rev`, a
content hash for optimistic concurrency. Full reference:
[Revisions](/llms/revisions.md).

---

### Annotation

A mark a person left on a place in the text: they read a document, reached a
sentence they did not believe, and said what is wrong with it. **It is a request
for work addressed to a place in the text, not a record of what we know** — that
is the whole border with provenance (`session_id`, written by the system) and
with anything the agent records about its own knowledge.

Two things follow, and both are enforced in `AnnotationService`, not by
convention: **no MCP tool creates one** (the web UI and `POST
/api/entries/{id}/annotations` are the only ways a mark is born), and **no agent
may close one**.

| Field | Type | Description |
|-------|------|-------------|
| `code` | string | Auto-assigned: `A1`, `A2`… (per research). Not a cross-reference target — `[[A1]]` resolves to nothing |
| `entry_id` | string | The document it marks. `research_id` is denormalized from it for the queue and authorizes nothing on its own |
| `block_id` | string | The block it is pinned to. **Empty for a markdown entry**, which has no addressable blocks; a block id sent for one is dropped rather than stored as a promise the document cannot keep |
| `quote` | object | `{exact, prefix?, suffix?}` — what was selected, with up to 120 characters either side to tell two identical sentences apart. `exact` is required, max 2000 characters (runes) |
| `anchored_revision` | int | The entry [revision](#revision) current when the mark was made. This is what `entry_diff` is run from when the text is gone |
| `kind` | enum | `verify` / `dig` / `disagree` — a closed vocabulary, because each is a different job |
| `body` | string | The person's note, max 5000 characters (runes) |
| `author_kind` | enum | `human` / `agent` — taken from the credential, never from the request |
| `status` | enum | `open` → `answered` → `closed` \| `dismissed` |
| `resolution` | string | What the agent did about it, max 5000. Parsed for `[[...]]` exactly as a task's description is, so `[[E19]]` becomes a real crossref |
| `resolved_revision` | int | The revision the answer left behind |
| `session_id` | string | The pass that answered it — taken from the entry's session, not invented |
| `task_id` | string | Set only by an explicit promotion; nothing links a mark to a task automatically. Many marks may point at one task |
| `attempts`, `rejections` | int, object[] | How many times an answer was sent back, and each refusal in full: `{reason, revision?, at}` |
| `anchor` | object | **Computed on every read, never stored** — see below |

**Lifecycle.** `open → answered → closed | dismissed`. There is deliberately no
priority, no `blocked`, no `deferred` and no `result`: those belong to
[Task](#task), and adding them here builds a second todo list against the first.
An agent may reach `answered` and no further — `closed` and `dismissed` are
refused with `only a person may close or dismiss an annotation` (`403`), because
an agent that accepts its own work turns the product into a self-confirmation
machine. Re-opening an `answered` mark counts an attempt and records the reason
beside the answer it refuses, never over it. A dismissed mark is kept, not
deleted: "we looked and decided it did not matter" is a finding.

**The anchor.** `block_id` is the address and the `quote` is the proof; when they
disagree, the disagreement is the finding. Every read places the marks against
the document as it stands now and reports `{state, strategy, confidence,
block_id, block_index, block_type, text}`:

| `state` | `strategy` | `confidence` | Means |
|---------|-----------|--------------|-------|
| `anchored` | `block_id` | `1` | Block found, quote inside it |
| `anchored` | `quote_in_block` | `0.6` / `0.9` | A markdown entry, which never had a block id: finding the quote is the healthy case, not a recovery |
| `drifted` | `block_id` | `0` | Block found, quote gone. The most valuable state: the text under a mark changed |
| `moved` | `quote_in_doc` | `0.6`, or `0.9` when the recorded prefix/suffix still surround it | The block is gone and the sentence turned up elsewhere |
| `orphaned` | `none` | `0` | Neither survives. `block_index` is `-1` so it does not sort under the first paragraph |

Matching is case-insensitive with runs of whitespace collapsed, and **matches are
consumed**: two marks on the same repeated sentence resolve to two different
occurrences. A `code`, `mermaid` or `html` block contributes no text, so nothing
can be anchored inside one — an `html` block renders in a sandboxed iframe where
a selection cannot be observed at all. An orphan is never garbage-collected: it
means the paragraph somebody doubted was rewritten, and only a person can say
whether the doubt was answered or the claim was buried.

**What a write reports.** An entry write compares the anchors before and after
and returns `annotation_report` with `annotations_drifted` and
`annotations_orphaned` (codes), mirroring `block_report`. Only a *transition* is
reported — a mark already orphaned is not news — and `moved` is not reported at
all. It rides on the entry, so `PUT /api/entries/{id}` and `POST
/api/entries/{id}/patch` carry it inside `data.annotation_report`, and the MCP
`entry_update` and `entry_patch` results return it beside the block report. A patch whose ops are all `set_state` computes nothing: a tick moves no
prose and can strand no mark.

**REST** — reads use `wrapRead`, writes `wrap`; none of them is on the share
sub-mux:

```
GET    /api/researches/{id}/annotations   the queue: ?status= ?kind= ?entry_id= ?anchor=
                                          {id} resolves an R-code. Answers data, count and
                                          meta {counts, by_anchor, by_entry}
GET    /api/entries/{id}/annotations      what is marked in this document
POST   /api/entries/{id}/annotations      create — {block_id?, quote{exact,prefix,suffix}, kind, body}
POST   /api/researches/{id}/annotations/bulk  one human decision over many ids: {ids, status, reason},
                                          at most 60. The ids decide which marks, each authorized
                                          on its own; 200 with a per-row report, because "12 of 14"
                                          is a real outcome and neither a success nor a failure
PUT    /api/annotations/{id}              body, kind, task_id, status (+ reason on a rejection),
                                          or block_id + quote to re-point a drifted mark by hand
DELETE /api/annotations/{id}
```

`?anchor=` is filtered after the read rather than in it, because anchor state is
computed from the document and cannot be a `WHERE` clause. `author_name` is
resolved only for a reader who is in the team that owns the research now — the
right to read a document is not the right to know who doubted it.

**Access** is the entry's: creating, answering, editing and deleting are all
writes, so a `viewer` reads marks and makes none. **No share route reaches a
mark**: who doubted which sentence is working process, like provenance and
revision history, and the `/api/shared/{token}/` sub-mux carries no annotation
route to reach one through.

Marks travel with nothing: no export carries them, including the portable dump,
so a research moved to another server arrives with its queue empty.

Full working guide, including what the two MCP tools return: [Annotations](/llms/annotations.md).

---

### CrossRef

Links between documents, extracted automatically from `[[...]]` patterns.

| Syntax | Meaning |
|--------|---------|
| `[[E3]]` | Entry E3 in same research |
| `[[R2:E5]]` | Entry E5 in research R2 |
| `[[R2]]` | Research R2 itself |
| `[[RM1]]` | Roadmap RM1 in same research |
| `[[RM1:N3]]` | Node N3 in roadmap RM1 |

**Where they render:** Anywhere the web UI shows markdown — entry content, question text/rationale/answers, task titles/descriptions/results, session notes.

**Where they are indexed** (stored in the `crossrefs` table, and therefore visible to the graph views and the crossref API):

| Source | Indexed text | When |
|--------|--------------|------|
| Entry | `content` | create, update, rebuild |
| Question | `answer` | `question_update` |
| Task | `description` + `result` | `task_update` (not on create) |

**Resolution:** A reference resolves when its target exists — and the target does not have to exist first. A reference written before its target is stored unresolved and is **repaired the moment the target is created**: creating an entry, task, roadmap, node or research points every waiting reference at it and emits `crossrefs.resolved`.

Only the qualified forms are matched across researches — `[[R3:E20]]` and `[[R2]]`, which carry a research code, and research codes are global. A bare `[[E20]]`, a `[[T4]]`, a `[[RM1]]` and a `[[RM1:N3]]` are matched **only inside the research they were written in**, because all four of those codes are allocated per research and repeat across them. An already-resolved reference is never silently re-pointed at a newer holder of the same code.

So an unresolved reference whose target now exists is a typo, a deleted target, or a code from another research — not a race. `POST /api/researches/{id}/crossrefs/rebuild` (MCP: `crossref_rebuild`) re-scans every source — documents, task results, question answers — and reports `{sources, references, unresolved}`; `rebuilt` is kept as an alias of `references`, having previously counted documents while being documented as references. It is the last resort: a restore, or codes backfilled onto records that predate them. `GET /api/researches/{id}/crossrefs` carries a `summary` beside the list — `total`, `unresolved`, `dangling` (the first 12 distinct codes that point at nothing) and `dangling_total` — all counted after the visibility filter, so the numbers describe what this reader can see.

**Visualization:** Shown on entry detail pages (outgoing/incoming), in the mindmap and the knowledge graph view (dashed / crossref edges), and preserved in export.

---

### Skill

A methodology document — how to run an interview, how to grade a source, how to build a roadmap — that a research *follows* and an agent opens at the moment it needs it. Private skills carry rules for **this research**; team and built-in skills explain how a **kind of work** is done, and the same skill can be followed by many researches without being copied.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | The address for management. A slug is not one: a built-in and a team's fork of it share a slug |
| `slug` | string | Derived from `name`. How a skill is addressed **inside a research**, where the resolution order is defined |
| `name` | string | Required |
| `tier` | enum | `private` / `team` / `builtin` — where it lives, and its precedence |
| `description` | string | The trigger line: **when** to load it, never what it contains. Max 200 characters (runes). The one field always in the agent's context |
| `body` | string | Markdown, max 16000 characters (runes), required. **Omitted from every list**; carried only by `skill_load` and `GET /api/researches/{id}/skills/{slug}` |
| `team_id` / `research_id` | string | The ownership pair: exactly one is set, or neither for a built-in |
| `user_id` | string | Who wrote it. A record, never a permission — as with a research |
| `ambient` | bool | A product skill: never counted against the cap, never detachable. Set from the shipped file at boot, not through the API |
| `forked_from` | string | The built-in slug this row copies, when it is a fork or a copy |
| `needs_trigger` | bool | The description was generated rather than written by a person; cleared on the first real edit |
| `body_tokens` | int | Estimate — characters over four. Derived at read time, never stored |
| `version` | int | |
| `attached` | bool | Library listings only: whether this research already follows it |
| `via_template` / `attached_at` | bool / string | Attached listings only. They describe the attachment, not the skill |

**Tiers, and what wins.** `private` (one research) over `team` over `builtin`. That order is both the sort of the index and the precedence: where two skills conflict, follow the higher one. Every `skill_load` response restates it in a `precedence` field.

**Attachment** is a row in `research_skills`, not a field on the skill. A research may follow **six** chosen skills — `ambient` ones are outside that budget, and writing a research-private skill spends it, because such a skill is attached on creation.

**No short code.** A skill is never referenced from content, so there is no `[[…]]` form for one.

**Ten MCP tools, and thirteen REST routes over the same service.** The index — slug, name, tier, description, no bodies — rides in `research_get`, and the product skills are unioned into it, so it is populated even for a research nobody has curated. `skill_load` is the only tool that returns a body; `skill_list`, `skill_attach`, `skill_detach`, `skill_create`, `skill_update`, `skill_fork`, `skill_copy`, `skill_promote` and `skill_delete` do over MCP what the routes under `/api/researches/{id}/skills…`, `/api/teams/{id}/skills` and `/api/skills/{skillId}` do over HTTP, refusing with the same codes from the same `service.SkillErrorCode` switch. Both transports go through `SkillService`, so the cap, the ambient rule and the tier checks are enforced once.

**A slug is fixed at creation.** `Slugify(name)` runs in `build` and nowhere else: `Update` rewrites the name, the description and the body, and never the slug. Uniqueness is per tier scope, which is why a fork keeps its parent's slug and a second fork in the same team is `slug_taken`.

**A share link never exposes a skill** — not the index, not a body. Which methodology a team follows is working process, the same class as private skills and memory. There are no skills routes under `/api/shared/{token}/`, `SkillService.Load` refuses a share context before it resolves the slug, and the index is empty for one — so a route added later still fails closed.

Full reference, including the route table and the conflict codes: [Skills](/llms/skills.md).

---

### Template

A **kickoff methodology**, read once before a research exists. It carries no sections, no questions and no rows: only the criteria an agent matches on and a markdown body saying what to ask the person before proposing anything, what structure to suggest, what good work looks like here, and when the research is finished. The model then designs the research itself. A template says how a kind of research is **started**; a [skill](#skill) says how a kind of work is **done**; private skills carry this research's specific rules.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | The address for management. A slug is not one: a fork keeps its parent's slug |
| `slug` | string | Derived from `name`. How a template is addressed at kickoff, and what `research_create` stamps |
| `name` | string | Required |
| `tier` | enum | `global` / `team` — ownership, not precedence: exactly one template is chosen and then it is done |
| `source` | enum | `builtin` / `user`. Splits the global tier: `builtin` ships in the binary and is rewritten at every boot, `user` was written on this instance and the refresh never touches it |
| `description` | string | One line for a picker |
| `when_to_use` | string | **Required.** What an agent matches on before it has read anything else. Max 240 characters (runes) |
| `when_not_to_use` | string | Same cap. Knowing when a methodology is wrong is what stops it being applied to everything |
| `body` | string | Markdown, required, max 24000 characters (runes). **Omitted from every list**; carried only by `template_get` and `GET /api/templates/{slug}` |
| `skills` | string[] | Slugs of the skills a research started this way should follow |
| `skills_resolved` | object[] | Single reads only: each slug as `{slug, name, description, ambient}`, or `{slug, missing: true}` when it resolves to nothing — shortening the list would hide a broken methodology |
| `team_id` / `user_id` | string | Empty for a global one. `user_id` records who wrote it and is never a permission |
| `forked_from` | string | The global slug this copies, when it is a fork |
| `research_count` | int | How many researches it has started, counted only among those **you** can read — a global template is everybody's, and an unscoped count would report one team's activity to another |
| `body_words` | int | Size hint, derived at read time, never stored |
| `version` | int | Bumped on every edit. What `research_create` stamps alongside the slug |

**Two tiers, and there is no third — but the global tier holds two kinds.** `team` belongs to one team and is theirs alone. `global` is visible to every team on the instance and is either `source: builtin`, shipped with the binary and refreshed at every boot, or `source: user`, added here through `POST /api/templates`. **A team still cannot publish server-wide**: a body steers a model, and lending one team's instructions to another team's kickoff needs a trust story this product does not have. Only the **operator** can, proved by the instance `api_token` — not a role, since no role grants it and a team `owner` is refused with `operator_required`.

**Editing what ships forks it.** `PUT` on a `source: builtin` template is `not_allowed` for everybody, the operator included — the next boot would rewrite the row, so permitting the edit would be permitting it to vanish. The fork route copies it into a team with the edit applied. A `source: user` global *is* editable, by the operator alone. The fork **keeps its parent's slug** — it is the same methodology, edited — and **shadows** the global in every list that team sees, so the same slug is never offered twice and resolution finds the team's copy first. Boot-time refresh only ever touches rows with no team **and `source: builtin`**, so an upgrade can overwrite neither a team's fork nor a global the operator wrote — if a shipped file ever takes a slug the operator already used, that one file is reported as a problem and skipped while the rest still load.

**Visibility is your memberships, never your request.** The list is the global set plus the libraries of the teams you belong to; a slug can never resolve into a team you are not in, and a lookup by id obeys the same rule rather than bypassing it. With `auth_enabled: false` there is nobody to scope to and everything on the instance is visible. The **operator reads less, not more**: a caller holding the `api_token` sees the global tier alone on `GET /api/templates` and `GET /api/templates/{slug}`, because the token proves who runs the server and never membership of a team — enough to hold the id of a global and edit or delete it, and no route into anybody's private library.

**Two MCP tools, `template_list` and `template_get`** — criteria for all, body for one. Everything else (write, fork, edit, delete, draft) is REST: nine routes under `/api/templates…`, `/api/teams/{id}/templates` and `/api/researches/{id}/templates/draft`. The web UI is **read-only**: `/templates` lists them grouped by origin and `/templates/{id}` shows one with its body. No MCP tool writes a template and no screen does either — every write is REST.

**No short code.** A template is never referenced from content, so there is no `[[…]]` form for one; at kickoff it is addressed by slug and everywhere else by id.

**A share link exposes no template** — not the list, not a body, not the stamp. `TemplateService` refuses a share context before it resolves anything, no template route is mounted under `/api/shared/{token}/`, and `redactForShare` blanks `template_slug` and `template_version` on the research a visitor reads, next to private skills and memory.

**Templates emit no events.** Writing, forking, editing or deleting one sends nothing over `/ws` — re-read `GET /api/templates`. Nor do the attachments a template makes: the skills `research_create` attaches from a `template_slug` are written without a `skill.attached` event, so a client that watches the socket to keep a skills index fresh must re-read it after creating a research from a template.

Full reference, including the route table, the draft skeleton and the conflict codes: [Templates](/llms/templates.md).

---

## Workflow Summary

```
0. template_list → template_get → the methodology to follow (optional, but read it before you design anything)
1. research_create → Research + Sections (+ template_slug: stamps provenance, attaches that methodology's skills)
2. skill_create → Set private working rules; research_update → Append initial memory
3. session_create → Session + initial Questions
4. question_update → Record answers
5. question_create → Follow-up questions (if needed)
6. entry_create → Synthesize answers into entries with [[refs]]
7. task_create → Track remaining work
8. roadmap_create → Build visual graphs for step-by-step guides, learning paths, or decision trees
9. research_update → Add to memory (insights for future sessions)
10. Repeat 3-9 for new sessions on uncovered areas
11. annotation_list → work the marks the person left on the text, annotation_answer each one (they close them)
12. Mark sections completed → research completed
```

## Short Codes

| Entity | Pattern | Scope | Used in URLs |
|--------|---------|-------|-------------|
| Research | `R1`, `R2` | Global | `/research/R2` |
| Section | `S1`, `S2` | Per research | query param |
| Entry | `E1`, `E2` | Per research | `/research/R2/entry/E3` |
| Session | `SS1`, `SS2` | Per research | `/research/R2/session/SS1` |
| Question | `Q1`, `Q2` | Per session | `/research/R2/session/SS1/question/Q3` |
| Task | `T1`, `T2` | Per research | — |
| Roadmap | `RM1`, `RM2` | Per research | `/research/R2/roadmap/RM1` |
| Node | `N1`, `N2` | Per roadmap | — |
| Annotation | `A1`, `A2` | Per research | the queue page, `/research/R2/annotations` |

`A` is the one prefix that is **not** a cross-reference target: `[[A1]]` is parsed as an entry code, finds no entry, and is stored unresolved. A mark is named in a report, never linked from content.

A revision has no short code: it is a plain number, 1-based per entry. A [share](#share) has none either — it is addressed by its token and never referenced from content. Nor does a [skill](#skill) or a [template](#template): both are addressed by slug where they are used and by id where they are managed.

## Real-time Events

`GET /ws` on the REST port (`:8088` by default) is a WebSocket that pushes one JSON message per mutation, whatever caused it — a browser write, a REST write, or an MCP tool call over stdio, SSE or Streamable HTTP, since all of them run in one process. An event is a notification, not a payload: it names what changed, and the client re-reads it over REST. There is nothing to send in the other direction: the server discards whatever a client writes (512-byte read limit) and expects only pong and close frames.

### Connecting

- With `auth_enabled` the handshake needs the same credential as every other endpoint — a JWT, an API key or an OAuth token — as `Authorization: Bearer …`, or as `?token=…` because a browser cannot set headers on a WebSocket handshake. Missing or unresolvable: `401`, no upgrade. With auth off no credential is asked for.
- The credential is re-checked on the live connection roughly once a minute, on the keepalive. If it stops resolving to the same user — key deleted, token expired — the socket is closed with code **4401**, in the application range on purpose: `4401` means authenticate again, an ordinary drop means reconnect, and a client that cannot tell them apart retries the first forever.
- A [share link](#share) connects with `?share={token}` — plus `?unlock=…` for a password-protected one — and no user credential of any kind. It is checked before the user branch and in both modes, because with auth off falling through would register the visitor as an ordinary local client and hand them the whole server's stream. An unresolvable token is `401`, no upgrade. The link is re-resolved on the same keepalive timer, so revoking it or letting it expire closes that socket with **4401** within the minute.
- `Origin` is checked whether or not auth is on. It must equal the request `Host`, be loopback (`localhost`, `127.0.0.1`, `::1`), or match the configured `base_url`; anything else is refused at the handshake (`403`). A client that sends no `Origin` — curl, a script, an MCP client — is allowed through: the check exists to stop a page the user happened to visit from opening a socket to their server, which with auth off would hand over the whole stream.

### Envelope

| Field | Type | Present |
|-------|------|---------|
| `type` | string | always — `entity.verb`, listed below |
| `entity` | string | always — `research`, `section`, `entry`, `entry_view`, `session`, `question`, `task`, `roadmap`, `team`, `crossref`, `share`, `skill`, `annotation` |
| `entity_id` | string | always — the id of the thing that changed, not of its parent |
| `research_id` | string | always present, empty for team-scoped events |
| `research_code` | string | when the id resolves — the same scope as a short code (`R7`), so a page routed as `/research/R7` can match an event without resolving a UUID first |
| `parent_id` | string | the entity this one hangs off, for the entities that are not addressable on their own. `annotation.*` only today: the **entry** the mark is attached to |
| `parent_code` | string | when the parent resolves — the same parent as a short code (`E12`), for the same reason `research_code` exists: a document page routed as `/research/R7/entry/E12` has no UUID to compare against |
| `actor_user_id` | string | who caused it; absent with auth off and for anything an agent did over stdio |
| `actor_client_id` | string | which tab caused it, when the writer sent `X-Client-Id` |
| `reason` | string | `access.*` only |
| `name` | string | `access.revoked` only — the name of the team or research that was lost |
| `at` | int | server send time, milliseconds since the epoch. Use it instead of receipt time: a tab waking from sleep takes the whole queued backlog at once and would date every event to the moment it woke |

`target_user_id` is not on the wire. Directed events exist (below), but the addressee learns nothing from being named that the delivery did not already tell them.

### Types

| `type` | `entity` | `entity_id` | Notes |
|--------|----------|-------------|-------|
| `research.created`, `research.updated` | `research` | research | |
| `research.transferred` | `research` | research | the research now belongs to another team |
| `section.created`, `section.updated` | `section` | section | |
| `entry.created`, `entry.updated`, `entry.deleted` | `entry` | entry | `entry.updated` also covers `entry_patch` and a revision restore |
| `entry_view.updated` | `entry_view` | entry for one acknowledgement; research for a bulk acknowledgement | Personal read state changed. With authentication it is directed to that reader alone, so their other tabs re-read the queue without telling another member what they read. With authentication off it reaches ordinary local connections for the one `local` reader. Never delivered to a share connection |
| `crossrefs.rebuilt` | `crossref` | research | `POST /api/researches/{id}/crossrefs/rebuild` finished. Every link in the research may have moved, so the graph and mindmap views re-read wholesale rather than patching. Never delivered to a share connection |
| `crossrefs.resolved` | `crossref` | research | a reference that named a code before anything carried it now points at the thing that does — a document, task, roadmap, node or research was just created and repaired it. Same entity as the rebuild, so a page listening for "the link table moved" needs no new case. Never delivered to a share connection either, and for a reason worth stating: these fire on task and roadmap creates, so delivering them would tell a visitor whose link excludes tasks that a task had just been made. A share view repaints on `task.created` / `roadmap.created` instead, which reach a visitor only when their link includes those parts |
| `session.created`, `session.updated` | `session` | session | |
| `question.created` | `question` | question | **one event per question**, so a batch of twelve sends twelve. It used to fire once per batch carrying the *session* id; a client written against that will now see the session id nowhere |
| `question.updated` | `question` | question | an answer or a status change |
| `task.created`, `task.updated`, `task.deleted` | `task` | task | |
| `roadmap.created`, `roadmap.updated`, `roadmap.deleted` | `roadmap` | roadmap | adding, changing or removing nodes and edges all report as `roadmap.updated` on the roadmap |
| `annotation.created`, `annotation.updated`, `annotation.answered`, `annotation.deleted` | `annotation` | annotation | `entity_id` is the **mark**, which tells an open document page nothing it can act on — the question that page asks is "is this one of mine", and only the entry answers it. So these are the events that carry `parent_id` / `parent_code`: the entry. `annotation.answered` is an agent finishing its work; `annotation.updated` covers everything a person does, closing and dismissing included |
| `skill.attached`, `skill.detached` | `skill` | skill | this research started or stopped following a skill. The index `research_get` returns has changed |
| `skill.created` | `skill` | skill | a research-private skill was written, or a team/built-in one was copied into this research. A **team** skill being written emits nothing — it belongs to no research yet |
| `skill.updated` | `skill` | skill | the body or trigger line changed. A fork and a promotion report as this too, on the **new** row — its `entity_id` is not the one you were following. Editing a team skill sends one of these **per research following it**, since the row itself names no single research |
| `skill.deleted` | `skill` | skill | the row is gone. Sent to every research that was following it, read before the delete because the attachments cascade away with it |
| `team.created`, `team.updated`, `team.deleted`, `team.invited`, `team.invite_revoked`, `team.member_added`, `team.member_removed`, `team.member_role_changed` | `team` | team | no `research_id` |
| `access.changed` | `team` | team | directed. `reason: role_changed`. The new role is deliberately not in the event — read it back from the API rather than trusting a value pushed at you |
| `access.revoked` | `team` or `research` | team or research | directed. `reason: removed_from_team` (entity `team`) or `research_transferred` (entity `research`, with `research_id`) |
| `share.created`, `share.updated`, `share.revoked` | `share` | share | a read-only link was handed out, changed or taken back. `share.updated` is a `label` or `include` change on a link that goes on working — the token is unchanged, so a manage list re-reads the row rather than expecting a new one. Delivered by the ordinary research rule, so any member who may read the research learns a link exists — and never to a share visitor, who has no use for the news and, in the revoked case, is being disconnected by it |

There is no `template.*` event of any kind: a [template](#template) belongs to a team rather than to a research, is read once at kickoff, and nothing on screen goes stale when one changes — re-read `GET /api/templates`. The skills a template attaches at `research_create` are written without a `skill.attached` event too.

There is no delete event for a research, section, session or question: none of them can be deleted. Only entries, tasks, roadmaps, teams, skills and annotations can. A share is revoked rather than deleted, which is why `share.revoked` and not `share.deleted`. A skill deleted by `skill_delete` or `DELETE /api/skills/{skillId}` sends `skill.deleted` to each research that was following it — the follower list is read before the row goes, because the attachment rows cascade with it and afterwards there would be nobody left to tell. Detaching a research-private skill deletes it too, and reports as `skill.detached`.

### Who receives what

Delivery is decided per event per connection, at send time — not once when the connection opens, which is what would let a revoked membership keep receiving updates until the tab was closed.

- With `auth_enabled: false` there are no users and one local team: every connection receives everything.
- With auth on, a research-scoped event goes to the users who may read that research — the same rule a REST read applies — and a team event to that team's members. An unidentified connection receives nothing.
- A **directed** event (`access.revoked`, `access.changed`, and `entry_view.updated` with authentication on) is addressed to one user and skips the research-wide audience. Access events use that path because the ordinary rule already refuses somebody who just lost access; the entry-view event uses it because one member's reading is not another member's business. Directed events therefore only occur with auth on.
- `access.revoked` carries `name` and `reason` because its recipient can no longer look either up — the moment it is sent, fetching what they lost answers 404.
- A **share** connection is scoped by the link, not by membership: events for its one research, filtered by the same `include` flags that gate its read routes — an event about a task on a link that excludes tasks is not harmless noise, it says a task exists and when somebody touched it. Team events, directed events, `share.*` events and `entry_view.*` events never reach it. The explicit entry-view rule also protects auth-disabled installations, where the local reader's event has no target user id. `actor_user_id` and `actor_client_id` are stripped from what a share receives: they name an account and a browser tab inside the owner's organisation, and there is no tab on the other side of a link to recognise its own writes.
- Membership verdicts are cached for at most a minute and dropped outright whenever anything touches a team, so the cache is never the reason someone keeps seeing what they lost.
- The broadcast queue is bounded. Under a burst the server drops events rather than stalling the write that produced them, so a client must be able to recover by re-reading, not by replaying.

### Recognising your own writes

A REST write may carry `X-Client-Id`: an opaque per-tab string, at most 64 characters (longer is truncated), invented by the client and never interpreted by the server. It comes back as `actor_client_id` on every event that write produced, so a tab can skip refetching what it already knows. `actor_user_id` cannot do this job — it is empty with auth off, and two tabs of one person share it, so one would suppress a change the other made and must display. MCP writes never carry a client id, so an agent's change always reads as somebody else's.
