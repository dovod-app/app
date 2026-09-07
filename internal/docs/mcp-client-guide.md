# MCP Client Guide

Practical guide for AI assistants and MCP clients interacting with the Dovod server via MCP tools. Covers tool conventions, team roles and what they let you write, the kickoff templates and the skills index and when to read each, the queue of marks a reader left on the text and how far you may take one, content formatting, nullable fields, the destructive tools and the confirmation contract that governs them, common pitfalls, and the difference between MCP and REST API access.

## Two Ways to Interact

The Dovod server exposes two interfaces. Use the one that matches your integration:

| Interface | When to use | Reference |
|-----------|-------------|-----------|
| **MCP tools** | Claude Desktop, Claude Code, Cursor, ChatGPT (via Streamable HTTP), any MCP client | This guide + tool descriptions returned by the server |
| **REST API** | Custom integrations, scripts, webhooks, non-MCP clients | [OpenAPI Specification](/api/openapi.yaml), or the same document as [JSON](/api/openapi.json) |

Both interfaces operate on the same data and produce the same results. MCP tools are thin wrappers around the same service layer that the REST API uses.

The spec is **generated from the routes the server registers**, so it describes
every one of them and is the complete reference — this guide does not repeat the
route list. It is also where the authentication contract is written: a REST
caller presents `Authorization: Bearer <token>` holding a JWT from
`POST /api/auth/login`, an API key from `POST /api/auth/api-keys`, or an OAuth2
access token from `POST /auth/token` — the three are interchangeable on every
route. The instance `api_token` is a separate credential and is not one of them:
it identifies whoever runs the server, belongs to no team, and only the
server-wide template routes accept it in place of a person. Fetch the spec from
the instance you are talking to, because it is built from that server's
configuration: where accounts are disabled it documents the writes as needing the
instance `api_token`, or as open where there is no `api_token` either, and it
omits the OAuth endpoints entirely. **Read that last case with the next section
in hand**: an instance with neither credential takes a write only from the
machine it runs on, which the document does not say.

Its `servers` entry is the relative `/` unless the operator configured
`base_url`, in which case it is that absolute URL. Relative is not a blank to
fill in: resolve the paths against wherever you fetched the document from, which
is what makes it right behind a reverse proxy, a tunnel or a non-default port.
Do not substitute a host of your own. The document is served with an `ETag` and
answers `304` to a matching `If-None-Match`, so a client that keeps a copy can
revalidate it cheaply rather than parsing it again on every run.

The personal document-update queue is deliberately outside that shared
research data. It records the numbered revision a person actually rendered in
the web UI, has REST routes only, and is absent from MCP, exports and public
shares. Do not acknowledge it on a user's behalf; use `entry_history` and
`entry_diff` when you need to understand what changed.

**MCP prompts** (`research/initialize`, `research/conduct`) return workflow instructions that tell you which tools to call in which order. `research/initialize` takes an optional `topic` argument; `research/conduct` requires `research_id`. They are the recommended starting point for new research projects, but every action they describe can also be done with individual tool calls.

## Connecting Over HTTP: Which Credential This Instance Wants

Over **stdio** nothing is asked of you: the client starts the process, and the
process is the boundary. Everything in this section is about the two transports
that arrive over a socket, and **the credential is the same one for both** — the
gate exists to protect the 57 tools, not a particular door, and a token that
closes one and not the other is not a credential:

- **Streamable HTTP** on the web port (`:8088` by default), which is what
  ChatGPT, Claude.ai and most current clients speak.
- **The legacy SSE transport** on the MCP port (`:8081` by default, path `/sse`),
  which exists only when the server was started with `--transport sse`.

On the Streamable HTTP side two paths reach the same handler behind the same gate
— `/mcp`, and the catch-all `/`, which hands a `POST` or `DELETE` carrying
`Content-Type: application/json` or an `Mcp-Session-Id` header, and a `GET` with
`Accept: text/event-stream`, to the MCP transport. Changing the path does not
change what is asked of you. **Post to `/mcp` anyway**: the catch-all compares
`Content-Type` for exact equality, so a request declaring
`application/json; charset=utf-8` is not recognised as MCP there and is answered
with the web UI's HTML instead.

| Instance | Send | Refusal if you do not |
|---|---|---|
| **Accounts on** (`auth_enabled`) | `Authorization: Bearer <token>` — a JWT from `POST /api/auth/login`, an API key from `POST /api/auth/api-keys`, or an OAuth2 access token from `POST /auth/token`; the three are interchangeable | `401 {"error":"unauthorized"}`. On SSE the refusal also carries `WWW-Authenticate: Bearer resource_metadata="…"`, which is where OAuth discovery starts |
| **Accounts off, instance `api_token` configured** | `Authorization: Bearer <api_token>` — the instance token, exactly as the REST writes take it | `401 {"error":"invalid or missing bearer token"}` |
| **Neither configured** | nothing — but only a caller on the machine the server runs on is let in | `401 {"error":"the write API is disabled: set api_token or auth_enabled to accept writes from another machine"}` |

**On SSE an *account* token may also travel as `?token=<token>`**, in the query
string, and that is not a shortcut for the lazy: the `EventSource` API cannot set
request headers, so for a browser-based SSE client it is the only place to put
one. The header is read first where both are present.

**The instance `api_token` is refused in the query string**, on every transport
including SSE — `?token=<api_token>` gets the same `401` as sending nothing. An
account token is scoped to one person and can be revoked on its own; the
instance token is the longest-lived, highest-privilege secret the server has, it
does not rotate, and a query string is written verbatim into every proxy access
log, browser history and `Referer`. Send it as `Authorization: Bearer
<api_token>` or not at all.

**The middle row is the one that breaks a client that used to work.** An instance
with an `api_token` and no accounts used to refuse an anonymous `POST
/api/entries` and then hand the same caller every tool through `tools/call`, on
either transport. Both doors now want that token. A connection that has started
failing with `invalid or missing bearer token` is not missing an account: ask the
operator for the instance `api_token` and send it as the bearer — as a header, on
SSE too. There is no account to register for on such an instance —
`POST /api/auth/register` does not exist there.

**"On the machine the server runs on" is exact, and it is four conditions.**

1. The connection's peer address is loopback (`127.0.0.1`, `::1`).
2. The request carries no `X-Forwarded-For`, `X-Real-IP` or `Forwarded` header.
   A reverse proxy adds one, and the proxy this project ships sits on the same
   host — so behind it every caller is remote whatever address the server sees.
3. The `Host` header names a loopback address (`localhost`, `127.0.0.1`, `[::1]`,
   with or without a port). **Reaching the same server by its LAN address or its
   hostname is refused** even from the same machine: `http://192.168.1.5:8088`
   and `http://my-laptop.local:8088` both fail this, and so does a name that
   resolves to `127.0.0.1` from outside. Use `localhost`.
4. `Origin`, when the request carries one, also names a loopback address. Without
   this the whole rule is bypassable by any web page the operator visits — the
   server answers with `Access-Control-Allow-Origin: *`, so a script on another
   site can reach `127.0.0.1` through the operator's own browser.

There is no header that makes you local; sending a forwarding header, or an
`Origin` from anywhere else, only makes you less so. This posture
exists for the single-binary local run, not for an exposed port, and it gates
**every tool**, reads included, because they reach the same services a write
does. It guards the SSE listener on the same terms, so moving to the other port
does not move you to another posture. REST *reads* are the exception in both
credential-less postures: with accounts off they take no credential from
anywhere, which is why a remote client can still fetch `/llms.txt`, the OpenAPI
document and a research over HTTP while being refused both MCP transports.

### Finding out which posture you are in

Both probes below live on the **web port**, never on the SSE listener, which
serves the transport and nothing else. An SSE client asks `:8088` what `:8081`
will want of it.

- `GET /api/auth/info` is public and answers on **every** instance, whether or
  not accounts are configured: `{"auth_enabled": …, "allow_registration": …}`.
  `auth_enabled: false` means accounts are off — it no longer means the route is
  missing, so do not read a failure to fetch it as "accounts are on". It carries
  `auto_login_token` only on a local instance running with `default_user`, and
  only for a caller on that machine.
- `GET /api/health` carries `write_api`, and it answers about **this request**,
  not about the instance: whether the server would accept a write from the
  caller asking. With accounts on it is `true` — the route carries no session to
  check, and your role decides the rest. With an `api_token` and no accounts it
  is `true` only when *this* request presented that token, so a browser holding
  none is told `false`, which is the truth and is why the web UI stops rendering
  Edit there. With neither configured it is `true` to a local client and `false`
  to a remote one, the same answer as the refusal above.
  It also carries `auth_enabled`, `version` and `in_memory` —
  `in_memory: true` means nothing survives a restart.

### A path that does not exist answers in JSON

Any path under `/api/` matching no route is `404 {"error":"no such endpoint"}` on
every method, `GET` included, and a method a real path does not serve lands
there too rather than in a `405`. Never read a `200` with an HTML body as an
answer from this API: that is the web UI, and a stale path used to return it.

## Available MCP Tools

### Research

| Tool | Purpose |
|------|---------|
| `research_create` | Create research with sections, tags, and goal. Optional `team_id` picks the team it lands in; omitted, it goes to your personal team. Optional `template_slug` records the methodology you followed and attaches the skills it names |
| `research_get` | Load full research context (sections with their `spec_version` and, where non-empty, their `instruction` and `field_spec`; entry counts; active session; and the skills index when the research follows any) |
| `research_resume` | The outstanding work: tasks in progress, blocked and pending, the open and deferred questions of one session, the marks a person left, the documents changed most recently, and up to three candidate next actions each carrying its reason and whether it is yours or a person's. Read-only — no session is created, no status moves, nothing is marked as seen. It carries no `memory`, `field_spec` or skills index: `research_get` owns those, and the two are meant to be called in that order |
| `research_list` | List every research you can reach, with optional status filter. Marks a shared one with `team` and a read-only one with `access: "read-only"` |
| `research_update` | Update metadata or append one note with `add_memory` and optional `session_id` |
| `research_delete_preview` | Counts what deleting this research would destroy, and which other researches cite it. Deletes nothing — read it before `research_delete` and tell the person the numbers |
| `research_delete` | **Destroys the research and everything in it. Permanent, owner only, `confirm: true` required.** See [Deleting Is Permanent](#deleting-is-permanent) before you call it |
| `research_memory` | List/add/edit/delete memory items by ID; edits require `version` |
| `research_add_section` | Add a new section to an existing research |
| `research_export` | Export a full research (sections, entries, sessions, questions, tasks, roadmaps). `format` defaults to `portable` (JSON for `research_import`); `format: "obsidian"` returns a link to download it as an Obsidian vault |
| `research_import` | Re-create a research from a portable export payload. Optional `team_id` picks the team it lands in; omitted, it goes to your personal team |

### Entries

| Tool | Purpose |
|------|---------|
| `entry_create` | Create an entry in a section — markdown by default, or a block document via `entry_type`. `metadata` carries values for the fields that section declares |
| `entry_read` | Read full entry content, plus the entry's `metadata` and its `metadata_status` against the section's current declaration |
| `entry_list` | List entries in a section (title/description/tags only — no content and no `metadata`) |
| `entry_update` | Update title, content, description, status, tags, session link, entry type, `metadata`, or replace text in a **markdown** entry (`text_replace`). `allow_incomplete` overrides the one refusal: completing a document whose required fields are unanswered |
| `entry_patch` | Edit blocks of a `blocks` entry by id — update, insert, delete, move, tick a checklist item — as one atomic, strict change. The surgical edit path for block documents; `text_replace` is refused on them |
| `entry_delete` | Delete an entry (also removes its cross-references and external links) |
| `entry_history` | List an entry's revisions: who wrote each one (agent, human, import, restore), in which session, and what it changed. Read it before rewriting an entry another session wrote |
| `entry_diff` | Unified diff between two revisions; with no numbers, the most recent change. Block documents are compared as markdown, not JSON |

**Taking one document out is not a tool.** A document can be downloaded as a markdown file with YAML front matter — `GET /api/entries/{id}/markdown`, or the **Download .md** item in the `⋯` menu on an entry page — and no MCP tool does it: you already have the content from `entry_read`, and putting a file on someone's disk is a human act. If a user asks for one, point them at the entry page. The file carries no provenance and does not rewrite `[[E3]]` for a foreign vault. [Export](/llms/export.md).

**Putting one in is not a tool either.** A markdown file can be dropped into a section — `POST /api/sections/{id}/import/preview` then `POST /api/sections/{id}/import`, or the **Import .md** button on the section view — and again no MCP tool does it. **Use `entry_create`**: you already have the text, and you can set `title`, `description`, `status`, `tags` and `metadata` directly instead of hiding them in YAML front matter that the importer then has to guess at, ignore or refuse. The import exists for a file on a person's disk, and its preview step exists so *they* can see what would be lost before committing. If a user asks, point them at the section page. Do not go looking for `entry_import`; there is none. [Export → One File into a Section](/llms/export.md).

### Sessions & Questions

| Tool | Purpose |
|------|---------|
| `session_create` | Create a Q&A session with initial questions |
| `session_get` | Load session with all questions and progress |
| `session_update` | Update title, focus, status, or notes |
| `session_delete` | Delete a session and the questions asked in it. Documents written during it survive, with their session link cleared |
| `question_create` | Add questions to an existing session |
| `question_update` | Record answer, change question status |
| `question_delete` | Delete one question. Follow-ups asked under it survive, with their parent link cleared |
| `question_list` | List questions with filtering |

### Tasks

| Tool | Purpose |
|------|---------|
| `task_create` | Create a todo item in a research |
| `task_list` | List tasks (sorted by priority) |
| `task_update` | Update status, priority, result |
| `task_delete` | Remove a task |

### Annotations

| Tool | Purpose |
|------|---------|
| `annotation_list` | The queue of marks a person left on places in the documents: the quote, the current text of the block it sits in, the kind of work asked for, and where the marked text is now. Never entry content |
| `annotation_answer` | Record what you did about one mark and move it to `answered` |

**Two tools, and the two that are missing are the design.** There is no
`annotation_create`: a mark is born from a person selecting a sentence in the web
UI, and a queue an agent can fill hides whether that gesture ever happened. And
nothing takes a mark past `answered` — `closed` and `dismissed` are refused with
`only a person may close or dismiss an annotation`, which over REST is a `403`.
You do the work; they accept it. [Annotations](/llms/annotations.md).

### Sections

| Tool | Purpose |
|------|---------|
| `section_list` | List sections for a research, each with `spec_version` and — only when the section has one — its `instruction` (how to write a document here) and the `field_spec` its documents record |
| `section_update` | Update section display name, description, status, position, `instruction` or `field_spec` (the slug `name` is immutable). The only way to set either: `research_create` and `research_add_section` take neither |
| `section_delete` | Delete a section. **Refused while it still holds documents** — the refusal names how many — unless `force: true` deletes them with it |

**Read the section's `instruction` before writing into it.** It is how *this*
section is written — "Name the producing service. State the consumer. One
paragraph of rationale, then the payload." — and both `section_list` and
`research_get` hand it to you, so nothing extra needs calling. It outranks the
research's memory and any skill on the narrow question of what a document filed
here looks like, and nothing wider than that. Writing one is `section_update`
with at most 500 characters; longer is refused, not trimmed. Where the section
also declares fields, the instruction names those keys. [Skills → Three places a
rule can live](/llms/skills.md), [Document Metadata](/llms/metadata.md).

### Templates

| Tool | Purpose |
|------|---------|
| `template_list` | The kickoff methodologies you may use — global plus your teams'. Matching criteria only, **no bodies**. Takes no arguments: send `{}` |
| `template_get` | One methodology in full, by `slug`. The only call that returns a body |

### Skills

| Tool | Purpose |
|------|---------|
| `skill_load` | Open **one** skill and return its full text, by `research_id` + `slug` or by `skill_id`. No batch form, and the only skill tool that returns a body |
| `skill_list` | What this research follows, what it may attach, and how much of the six-slot budget is spent. The call to make before changing anything. Pass `team_id` instead of `research_id` for a team's whole library |
| `skill_attach` / `skill_detach` | Start or stop following a skill, by `slug`. Detaching a **research-private** skill deletes it |
| `skill_create` | Write one: `research_id` for a skill private to this research (attached at once, spends a slot), `team_id` for the team library (attached to nothing) |
| `skill_update` | Edit name, trigger line or body in place; omitted fields are inherited. A built-in is refused — editing one is `skill_fork` |
| `skill_fork` / `skill_copy` | Take an editable copy: a built-in into the team library (`fork`), a team or built-in into this research (`copy`). The attachment moves in the same call |
| `skill_promote` | A research-private skill into the team library; the attachment follows and the private original is deleted |
| `skill_delete` | Remove a team or private skill from existence — as opposed to detaching it from one research |

### Teams

| Tool | Purpose |
|------|---------|
| `team_list` | The teams you belong to and your role in each. Use the id with `research_create` / `research_import` |

### Roadmaps

| Tool | Purpose |
|------|---------|
| `roadmap_create` | Create a full roadmap with nodes and edges. Set `view` (graph / stages / timeline) and `stages` for the non-graph layouts; nodes take `stage`, `node_date`, and `node_end_date` (with `node_date`, a timeline range bar) |
| `roadmap_get` | Load roadmap with all nodes, edges, and reference data |
| `roadmap_list` | List roadmaps for a research |
| `roadmap_update` | Update title, description, statuses, stages, view, status |
| `roadmap_delete` | Delete a roadmap and all its nodes/edges |
| `roadmap_add_nodes` | Add nodes and edges to an existing roadmap (new nodes take `stage`, `node_date`, and `node_end_date`) |
| `roadmap_update_node` | Update a single node — status, title, description, type, position, `stage`, `node_date`, `node_end_date` |
| `roadmap_remove_nodes` | Remove nodes (edges auto-cascade) |

## Deleting Is Permanent

There is no trash, no tombstone and no restore. A delete removes rows; the only
thing that brings the work back is writing it again. This is the one part of the
tool surface where a plausible-sounding call is not recoverable, so it has its
own rule:

**Delete only what you were asked to delete, in those words.** "Tidy this up",
"we don't need that any more" and "clean up the old sections" are requests to
*ask which* — never instructions to delete. Where somebody wants a finished
research out of the way, `research_update` with `status: "archived"` is
reversible and is almost always what they meant; the same goes for
`session_update` with `status: "completed"` and `question_update` with
`status: "skipped"`, which keep the record of the work rather than removing it.

The tools that destroy something: `research_delete`, `section_delete`,
`session_delete`, `question_delete`, `entry_delete`, `task_delete`,
`roadmap_delete`, `roadmap_remove_nodes` and `skill_delete` — plus
`skill_detach`, which deletes a research-private skill because it exists nowhere
else.

### `research_delete`: the confirmation is the contract

- **`confirm: true` is required.** It is a required, nullable boolean: `null` and
  `false` both refuse, with a validation result telling you to ask first, and
  nothing is deleted. Set it **only** when the person named this research and
  asked for it to be gone. Never infer it from a sentence that merely sounded
  like a tidy-up.
- **Owner only.** An `editor` gets `your role in this team does not allow this`;
  somebody with no access at all gets `not found`, because confirming the
  research exists is itself information.
- **Call `research_delete_preview` first and report what it says.** "Delete R7"
  and "delete 4 sections, 12 documents and 3 sessions" are different decisions,
  and the second one is the true one. The summary is
  `{sections, entries, sessions, questions, tasks, roadmaps, annotations,
  shares, incoming_refs, incoming_from}` — `incoming_from` names up to ten of the
  researches that cite this one, by `code` and `name`.
- **What goes with it**: sections, documents and every revision of them, sessions
  and questions, tasks, roadmaps with their nodes and edges, annotations, the
  research's memory, its share links (which stop working immediately) and the
  methodologies attached to it — a research-private skill is destroyed, a team or
  built-in skill only stops being followed.
- **What does not go: references from other researches into this one.** A
  `[[R1:E5]]` written in R2 stays in R2's text exactly as somebody wrote it and
  stops resolving. Deleting those rows would edit a research nobody asked to
  change. See [Cross-references](#cross-references).
- The answer is `{deleted: true, destroyed: {…the same summary…}, unresolved_in:
  [{code, name}]}`. Tell the person what went and which of their other researches
  now carry dead references, rather than "done".

### `section_delete`: `force` is the whole safety rail

- Without `force`, a section holding documents is **refused**, and the refusal
  names the count: `section is not empty: 3 document(s) would be deleted with
  it` (a `409` over REST). Nothing is deleted.
- `force: true` deletes the documents with it, along with the references and
  external links they wrote. References *to* those documents from elsewhere
  survive as text and stop resolving.
- **There is no way to empty a section except by deleting its documents.** A
  document cannot be moved between sections anywhere in this product — there is
  no `entry_move`, and `entry_update` does not accept a `section_id`. So "empty
  it first" is not the gentler path it sounds like; if the documents matter, keep
  the section.
- `force` is a required, nullable boolean like `confirm`: `null` means `false`.

### `session_delete` and `question_delete`: what survives

- **Documents written during a session survive** a `session_delete`, with their
  `session_id` cleared. A finding is not an artefact of the conversation that
  produced it. The session's questions do go, and so does everything they
  recorded — the answers are the part that is lost.
- **Follow-up questions survive** a `question_delete`, with their `parent_id`
  cleared; they do not disappear with their parent.

### Which ids these tools take

`research_delete` and `research_delete_preview` resolve a UUID **or** an `R1`
code, like the other research tools. `section_delete`, `session_delete` and
`question_delete` take **UUIDs only** — an `SS4` reads as `not found` here even
though `session_get` resolves it, and section codes are not returned by any tool
at all. Get the UUID from `research_get` / `section_list` (sections),
`session_get` or `research_resume` (sessions and questions) first.

## Access: You Can See More Than You Can Write

`team_list` names the teams you belong to and your role in each. Pass a `team_id` to `research_create` (or `research_import`) to put new work where your colleagues can see it — without it every research lands in your personal team and someone has to move it by hand.


A research is owned by a **team**, and your role in that team decides what you may do to it. You may therefore be able to read a research, list its entries and export it, and still be refused when you try to write to it. The creator of a research has no standing privileges over it: `user_id` records who made it and is never consulted for permission.

| Role | May do |
|------|--------|
| `viewer` | Every read tool (`*_get`, `*_list`, `*_read`, `entry_history`, `entry_diff`, `research_export`, `research_delete_preview` — the preview is a read, so it can say what the delete you may not perform would take) |
| `editor` | The above, plus every create/update/delete tool — including `section_delete`, `session_delete`, `question_delete` and `entry_delete` — but **not** `research_delete` |
| `owner` | The above, plus `research_delete`, team management and moving a research to another team (REST only) |

**How you find out, before you fail:**

- `research_list` marks each item it needs to. A research owned by a team that is not your own personal one carries `team` with the team's name; one you may only read carries `access: "read-only"`. **No `access` key means you may write to it.**
- `research_get` returns the research record itself, which carries `team_id`, `team_name`, `team_is_personal` and `role` (`viewer` / `editor` / `owner`).
- With `auth_enabled: false` there is no caller and no check: `access` never appears, and every tool is permitted.

**How a refusal arrives** — as an ordinary tool result with `isError: true`, never as a protocol error:

| Text | Means | Do |
|------|-------|----|
| `your role in this team does not allow this` | You are in the team but only a `viewer` — or you are an `editor` and the call was `research_delete`, which only an `owner` may make | Do not retry. Tell the user which research it was and that they need editor rights, or pick another research |
| `not found` | Either no such id, **or** it belongs to a team you are not in — the two are deliberately indistinguishable | Re-run `research_list` and use an id from it |

`research_create` and `research_import` both take an optional `team_id`. Omit it and the research lands in your own personal team, so a research you created this session is always writable. Name a team and it goes there instead — you must be an `editor` or `owner` of it: a team you are not in is `not found`, one where you are only a `viewer` is refused. Use `team_list` to get the id; `research_create` echoes back `team` with the team's name when the research did not land in your personal one.

**Share links are not yours to issue.** A research can be published as a revocable, read-only link that someone opens without an account, but there is no MCP tool that creates, lists, changes or revokes one — it is done in the web UI or over REST (`POST /api/researches/{id}/shares`), by a person deciding to give something away. If a user asks for one, point them at the share dialog on the research page rather than looking for a tool. Nothing you do over MCP is affected by a link existing, and a share token never authenticates an MCP call. [Domain Guide → Share](/llms/domain-guide.md#share).

Full model — roles, invitations, transfer and the team REST routes: [Domain Guide](/llms/domain-guide.md#team).

## Templates: Read One Before You Design a Research

A template is a **kickoff methodology** — the criteria an agent matches on plus a
markdown body saying what to ask the person before proposing anything, what
structure to suggest, and when the research is finished. It clones nothing: no
sections, no questions, no tasks come out of one. You read it and design the
research yourself.

Two tools, and only at the start of a new research:

- `template_list` takes **no arguments** — send `{}`. It answers
  `{templates: [{slug, name, tier, description, when_to_use, when_not_to_use}], usage_hint}`,
  never a body. An empty list is a normal answer, not an error.
- `template_get(slug)` answers
  `{slug, name, tier, when_to_use, when_not_to_use, body, skills, version, usage_hint}`.
  `slug` is a plain required string, taken from `template_list`; one you invent is
  `not found`.

Then pass `template_slug` to `research_create`. It is the only structural thing a
template does: it stamps `template_slug` and `template_version` on the research
and attaches the skills that methodology names, answering with `skills_attached`
and — when something could not be attached — `skills_unavailable`. Neither key
appears when empty, and neither failure ever fails the creation.

**Ask what decision is waiting before you offer a template.** A structure shown
before the person has said what they are deciding anchors them to it. And nothing
re-reads the body later: it steers the research only through the structure you
design and the skills it attached. **You cannot write, fork or delete a
template** — those are REST acts, like share links. (Skills are not: since the
skill tools landed, every skill act has an MCP equivalent.) The web
UI reads them and never writes one: point a person at `/templates` to see what is
available and `/templates/{id}` to read a body, not to edit one.
[Templates](/llms/templates.md),
[Domain Guide → Template](/llms/domain-guide.md#template).

## Skills: An Index You Are Given, A Body You Ask For

A research may **follow** skills — methodology documents saying how a kind of work is done (running an interview, grading a source, building a roadmap), with research-specific rules in private skills and reusable rules in team/built-in skills.

`research_get` returns them as `skills`: `slug`, `name`, `tier` and a `description` that says **when** to use each one. No bodies. Alongside it comes `skills_hint`. **The index is never empty in a working install** — the four product skills are in it whether or not anybody attached anything — so a missing `skills` key means the built-ins failed to load, not that this research follows nothing.

When you are about to do the work one of those lines names, call `skill_load(research_id, slug)` and read the body then. Not while orienting, and not all of them up front: that is the cost this design exists to avoid, which is also why the tool takes one slug and has no batch form.

- Both parameters are plain required strings — no `null`, and both must be non-empty.
- `research_id` accepts either the UUID or the short code (`R1`) — it is resolved, precisely because the slug you are passing came out of a `research_get` you may have made with a code.
- The response is `{slug, name, tier, description, body, precedence}`. `precedence` restates the one rule about conflicts: a skill attached to this research directly beats a team skill, which beats a built-in.
- A slug you invent, or one belonging to another research's private skill, is `not_found` — with the slug quoted and a pointer to `skill_list`, because a slug is fixed at creation and cannot be derived from a name.
- Pass `skill_id` instead of `research_id` + `slug` for a team skill attached to no research yet, which has nothing to be looked up through. By id, "no such skill" and "not yours" are one refusal and it invites you nowhere.

**You can change which skills a research follows**, and nine tools do it: `skill_list`, `skill_attach`, `skill_detach`, `skill_create`, `skill_update`, `skill_fork`, `skill_copy`, `skill_promote`, `skill_delete`. Each takes the research UUID or the `R1` code. Start with `skill_list(research_id, query?)` — one read gives `following`, `available`, `chosen`, `cap`, and `cap_reached: true` with a `cap_hint` when the budget is spent — then act. Pass `team_id` instead for a team's whole library, including what no research follows yet. Four things to know before you call one:

- **Six chosen skills per research.** The seventh attach is refused with `skill_cap_reached`, and so is writing a seventh private skill, because a private skill is attached the moment it is created. Detach something first. The product skills are outside the budget, so they are not what you drop.
- **`skill_detach` on a research-private skill deletes it** — it exists nowhere else, the answer says `deleted: true`, and nothing restores it. A team or built-in skill only stops being followed.
- **A slug is fixed at creation.** `skill_update` changes the name and never the slug, so renaming is not a way around `slug_taken`; edit or delete whatever already holds the slug.
- **A refusal leads with its machine-readable code** — `skill_cap_reached`, `already_attached`, `slug_taken`, `skill_in_use`, `not_allowed` — the same vocabulary the REST API uses. Match on the code, not the sentence: `skill_cap_reached` means drop something first, `already_attached` means carry on.

The four product skills need no attaching: they are in the index of every research already, and detaching one is `not_allowed`. [Skills](/llms/skills.md), [Domain Guide → Skill](/llms/domain-guide.md#skill).

## Nullable and Optional Fields

Read this before composing any tool call. Input schemas are generated from Go structs, and the generated schema lists most properties of a tool (and of nested objects) in `required`, with `additionalProperties: false`. Usually optionality is expressed by nullability; the omission exceptions below also allow absence:

| Parameter kind in the schema | How to skip it | Effect of `null` |
|------------------------------|----------------|------------------|
| `"type": ["null", "string"]` / `["null","number"]` (pointer in Go) | send `null` | default value is used |
| `"type": ["null", "array"]` (any list: `tags`, `statuses`, `questions`, `nodes`, `edges`, `node_ids`) | send `null` or `[]` | treated as empty |
| `"type": ["null", "object"]` (`text_replace`, `metadata`) | send `null` | the value is left alone |
| `"type": ["null", "boolean"]` (`allow_incomplete`, `confirm`, `force`) | send `null` | `false` — which for `confirm` and `force` means the call is refused rather than performed |
| `"type": "string"` / `"integer"` (plain scalar) | send `""` or `0` — **not** `null` | rejected: `null` is not a valid string/integer |

Consequences:

- **Send every required property.** Omitting one fails schema validation with `-32602 invalid params: required: missing properties: [...]` before the tool code runs. Use `null` (or `""` / `0`) rather than leaving a property out.
- **Thirteen exceptions**, the only tools whose schema does not require everything: `entry_history` (`limit`), `entry_diff` (`from`, `to`), `research_export` (`format`), `research_import` (`team_id`), `research_create` (`team_id`, `template_slug`), `research_update` (`add_memory`, `session_id`), `research_memory` (`text`, `item_id`, `version`, `ids`, `session_id`), `skill_create` (`research_id`, `team_id`), `skill_fork` (`name`, `description`, `body`), and `skill_list`, `skill_load`, `skill_update` and `skill_delete` (every property — each is addressed two ways and validates that itself). The new memory tools distinguish omission from null: `research_update.session_id` and `research_memory.text`, `item_id`, `version`, `session_id` may be omitted but reject `null`. `research_update.add_memory` and `research_memory.ids` also accept `null`. The other omission exceptions accept `null`.
- **A schema that requires nothing is not a tool that accepts nothing.** `skill_update` and `skill_delete` require no property because the skill may be addressed two ways — `research_id` + `slug`, or `skill_id` — and the tool checks that itself: give one form or the other, never both and never neither. `skill_create` is the same shape for ownership, `research_id` or `team_id`. `skill_update` also refuses a call that changes nothing: send at least one of `name`, `description`, `body`.
- **Two tools take no input at all**: `template_list` and `team_list`. Their schemas are empty objects — send `{}`.
- **Never send `null` into a plain scalar.** The optional-but-not-nullable parameters are: `research_create` → `description`, `goal`; each `sections[]` item → `display_name`, `description`, `position`; `research_add_section` → `display_name`, `description`, `position`; each question item → `position`; `research_update` → `session_id`; `research_memory` → `text`, `item_id`, `version`, `session_id`.
- **List filters are nullable**: `research_list.status`, `entry_list.status`, `question_list.status` / `area` / `priority`, `task_list.status` / `priority`, `annotation_list.status` / `kind` / `entry_id` / `limit` / `offset`. `null` or `""` means "no filter".
- **The delete tools are in the ordinary regime too.** `research_delete` carries `research_id` (a plain string) and `confirm`; `section_delete` carries `section_id` and `force`; `session_delete` and `question_delete` carry one plain string each. Send every property. `confirm: null` and `force: null` are the same as `false`, and both are safe: the tool refuses and deletes nothing.
- **The two annotation tools are in the ordinary regime**, not among the exceptions above: send every property. `annotation_list` carries one plain string (`research_id`) and five nullable filters, so the queue read is `research_id` plus five `null`s. `annotation_answer` carries two plain strings — `annotation_id` and `resolution`, neither of which may be `null` or empty — and one nullable `task_id`.
- **`research_resume` is in the ordinary regime as well.** `research_id` is a plain string, and it does resolve an `R1` code; `session_id` and `limit` are nullable, so the ordinary call is the research plus two `null`s. `session_id: null` selects the one active session, or returns the candidates with `selection_required` when several are open — it never picks for you. `limit: null` is 5, and a number outside 1–15 is clamped rather than refused.
- **`null` and empty are different for a replacing field.** `metadata` (`entry_update`), `field_spec` and `instruction` (`section_update`) are nullable but not "empty means empty": `null` leaves what is stored alone, while `{}` clears every value, `[]` removes every declared field and `""` removes the instruction. Send `null` unless you mean to erase.
- **Inside a `field_spec` item**, `key`, `label`, `type` and `required` must be present. `repeated`, `options` and `help` may be omitted; if you do send them, `options` accepts `null` while `repeated` (boolean) and `help` (string) do not — send `false` and `""`.
- Unknown property names are rejected outright (`additionalProperties: false`).

### Default values when a nullable field is null

| Field | Default |
|-------|---------|
| `priority` (question, task) | `medium` |
| `status` (entry) | `draft` |
| `node_type` | `step` |
| `edge_type` | `default` |
| `title` (entry) | Auto-generated from first non-empty line of content |
| `description` (entry) | Auto-generated from lines 2-5 of content |
| `position_x`, `position_y` | `0` (frontend auto-layouts) |
| `session_id` (entry) | The research's currently active session, if there is one |
| `metadata` (`entry_create`) | No values recorded. `entry_update`: the stored values are left as they are |
| `allow_incomplete` (`entry_update`) | `false` — completing a document with required fields unanswered is refused |
| `confirm` (`research_delete`) | `false` — the call is refused and nothing is deleted. There is no default that deletes |
| `force` (`section_delete`) | `false` — a section that still holds documents is refused, with the count in the message |
| `field_spec` (`section_update`) | The section's declaration is left as it is |
| `instruction` (`section_update`) | The section's writing instruction is left as it is. `""` removes it; over 500 characters the whole call is refused rather than truncated |
| `limit` (`entry_history`) | `20` newest revisions; the result says `truncated: true` when more exist |
| `format` (`research_export`) | `portable` — the JSON `research_import` takes. `obsidian` returns a vault download link instead; `json` / `vault` / `zip` are accepted aliases, anything else is a validation error |
| `team_id` (`research_create`, `research_import`) | Your personal team |
| `template_slug` (`research_create`) | No methodology recorded and no skills attached — the research is created either way |
| `query` (`skill_list`) | No filter — the whole library this research may attach. Ignored with `team_id` |
| `research_id` / `team_id` (`skill_create`) | Neither is a default: exactly one must carry a value, and it decides whether the skill is research-private or a team's |
| `name`, `description`, `body` (`skill_update`, `skill_fork`) | Inherited from the stored skill. `null` leaves a field alone; it is not a way to blank one |
| `status` (`annotation_list`) | `open` — the queue is a work list, so "the annotations" means the ones still wanting an answer. Any other value must be one of `open` / `answered` / `closed` / `dismissed` |
| `kind`, `entry_id` (`annotation_list`) | No filter — every kind, every document in the research |
| `limit` (`annotation_list`) | `15`, which is also the server's ceiling: a larger number is silently clamped to it, so a batch is never bigger than a diff a person will review |
| `offset` (`annotation_list`) | `0`. The result carries a `hint` when it came back full, which is when there may be more |
| `task_id` (`annotation_answer`) | The mark is not attached to a task. Promotion is explicit and never automatic |
| `to` (`entry_diff`) | The newest revision |
| `from` (`entry_diff`) | The revision before `to` — so a call with neither shows the most recent change |

### Example: creating a session with the fewest meaningful values

`research_id`, `title`, and each question's `text` are the only fields carrying information; everything else is still present, as `null` or `0`:

```json
{
  "research_id": "uuid-here",
  "title": "Initial exploration",
  "focus": null,
  "questions": [
    { "text": "What are the main components?", "area": null, "rationale": null, "priority": null, "parent_id": null, "position": 0 },
    { "text": "How do they interact?", "area": null, "rationale": null, "priority": null, "parent_id": null, "position": 1 }
  ]
}
```

`research_id` here must be the UUID — `session_create` does not resolve `R1`-style codes (see Short Codes).

## Content Formatting

All content fields that support markdown (`content` in a markdown entry, `answer` in questions, `notes` in sessions, `description` and `result` in tasks) follow these rules.

They do **not** apply to an entry with `entry_type: blocks`: there `content` is a JSON block document, markdown is not parsed, and each block field has its own rules — see [Block Documents](/llms/blocks.md).

### Newlines

**Use actual newline characters**, not literal `\n` sequences. The server normalizes literal `\n` to real newlines as a safety net, but it is better to send properly formatted content from the start.

```
Good:  "# Title\n\nParagraph one.\n\nParagraph two."
       (where \n is an actual newline character in the JSON string)

Bad:   "# Title\\nParagraph one.\\nParagraph two."
       (literal backslash-n — will be auto-corrected but indicates a client bug)
```

When composing multi-line markdown, ensure your JSON serializer encodes newlines as `\n` (JSON escape for real newline), not as `\\n` (escaped backslash + n).

### Markdown support

Content is rendered using GitHub Flavored Markdown with `breaks: true` (single newline = line break). Supported formatting:

- Headings (`# H1` through `###### H6`)
- Bold, italic, strikethrough (`**bold**`, `*italic*`, `~~strike~~`)
- Lists (ordered and unordered, nested)
- Code blocks (fenced with triple backticks, language hint supported)
- Tables (GFM pipe syntax)
- Links and images
- Blockquotes
- Mermaid diagrams (fenced code blocks with `mermaid` language — see below)

### Mermaid diagrams

Use fenced code blocks with the `mermaid` language identifier to embed diagrams. They are rendered as interactive SVG in the web UI. All Mermaid diagram types are supported: flowchart, sequence, class, state, ER, Gantt, pie, mindmap, timeline, etc.

Example in entry content:

```
\`\`\`mermaid
graph TD
    A[Start] --> B{Decision}
    B -->|Yes| C[Action 1]
    B -->|No| D[Action 2]
    C --> E[End]
    D --> E
\`\`\`
```

Tips:
- Keep diagrams focused — large diagrams become hard to read
- Combine diagrams with markdown text for context
- Use descriptive node labels
- Mermaid blocks work in entry content, question answers, session notes, and task descriptions/results
- Diagrams are interactive: drag to pan, ctrl/⌘ and scroll to zoom, a button for fullscreen,
  and a link that reopens the diagram in mermaid.live
- A diagram that fails to parse keeps its source and links to the editor, which reports the
  syntax error
- In a `blocks` document diagrams get their own block type: `{"type": "mermaid", "data":
  {"code": "flowchart TD\n  A --> B", "caption": "optional"}}`. A `code` block with
  `language: "mermaid"` is accepted as the same thing

### Cross-references

Use `[[...]]` syntax in content to create links between documents:

| Syntax | Target |
|--------|--------|
| `[[E3]]` | Entry E3 in same research |
| `[[R2:E5]]` | Entry E5 in research R2 |
| `[[R2]]` | Research R2 |
| `[[RM1]]` | Roadmap RM1 in same research |
| `[[RM1:N3]]` | Node N3 in roadmap RM1 |

`[[...]]` is rendered as a clickable link anywhere the web UI displays markdown: entry content, question text, rationale and answers, task titles, descriptions and results, session notes.

Only three sources are additionally **indexed** into the `crossrefs` table, and therefore feed the graph views and `GET /api/researches/{id}/crossrefs`:

| Source | Indexed text |
|--------|--------------|
| Entry | `content` (on create, update, and crossref rebuild) |
| Question | `answer` (on `question_update`) |
| Task | `description` + `result` (on `task_update`) |

Put references you want in the knowledge graph into entry content: the graph view draws an edge only for a resolved reference whose target is an entry, so `[[R2]]` and `[[RM1]]` are stored and clickable but never become graph edges. A reference to a target that does not exist yet is stored unresolved and can be fixed later with `POST /api/researches/{id}/crossrefs/rebuild`, which re-scans entry content only.

**A reference into something that was deleted is not removed.** Deleting a
research, or a section with `force`, clears the rows *its* documents wrote and
marks every row pointing *at* them unresolved — but the rows stay, and so does
the `[[R1:E5]]` in the citing document's own text. Nothing rewrites somebody
else's document to hide that the thing it cited once existed. Two consequences
for you: `GET /api/researches/{id}/crossrefs` will show those rows with
`resolved: false` and no target, and `crossrefs/rebuild` will not repair them,
because there is nothing left to resolve to. If you are asked to clean up after a
deletion, edit the citing text yourself — and only if you were asked.

## Entry Types and Statuses

Entries live in sections and represent synthesized research findings.

### Types

`entry_type` picks how `content` is interpreted. It is optional on `entry_create` and defaults to `markdown`.

| Type | `content` holds | Rendered as |
|------|-----------------|-------------|
| `markdown` (default) | Markdown, with `[[E3]]` cross-references and ```mermaid blocks | Markdown in the page |
| `blocks` | A block document `{version:1,blocks:[{type,data}]}` | Each block by its own renderer |
| `artifact` | A complete, self-contained HTML document | Sugar: stored as a `blocks` document holding one `html` block |

Use `blocks` when the entry is a composed document — prose plus an alert, a table, a chart, a checklist, a diagram, a transcript, a hand-built visual — rather than one stream of text. Fourteen block types are available: `paragraph`, `heading`, `list`, `table`, `quote`, `code`, `callout`, `divider`, `image`, `checklist`, `mermaid`, `html`, `task_ref`, `transcript`. Two of them point outside the prose: `task_ref` projects existing tasks into the document as a checklist whose ticks are status changes on the tasks (reference them by the uuid `task_create` returns), and `transcript` holds a conversation that happened outside the tool as `{speaker?, text, ts?}` turns. Text fields carry the inline markdown subset and `[[E3]]` references, and they are indexed; `code`, `mermaid` and `html` bodies are not. **Read [Block Documents](/llms/blocks.md) for the field-by-field catalog before writing one** — an unknown type or a mistyped field is dropped silently, so a guessed field name means a missing block.

Editing one: send the whole document again in `content` (forgiving — bad blocks are dropped) or change part of it with `entry_patch`, which addresses blocks by their `id` and is strict and atomic. `text_replace` does **not** work on a blocks entry and is rejected: it would run a string replacement over the stored JSON. Keep the block `id`s and the `checklist` item `key`s you read back — they are what a human's ticks hang on, and `entry_update` reports nothing when they are lost. `set_state` ticks a `checklist` item and nothing else: a `task_ref` row is ticked with `task_update`, which is where that status lives.

`artifact` still works and needs no document: pass the HTML as `content` and it is wrapped in one `html` block, taking its title from `<title>`. Reading the entry back returns `entry_type: blocks` — nothing stores `artifact` any more.

Everything else stays `markdown`.

Writing HTML — as an `html` block inside a document, or via the `artifact` alias:

- Send one whole document: `<!doctype html>`, `<head>`, `<style>`, `<script>`, `<body>`. Inline everything — external requests are not available to the frame.
- Give it a `<title>`. Using the alias, the entry title comes from there when `title` is omitted, and `<meta name="description">` fills `description` the same way. Inside a document, prefer the block's own `title` field.
- Scripts run. The frame is sandboxed with `allow-scripts` and without `allow-same-origin`, so the document cannot read cookies, storage or the host page — and cannot fetch from the API.
- The frame reports its own height, so it is shown in full with no inner scrollbar. Do not set `height: 100%` on `body` expecting a viewport; lay out for a document that grows.
- Read-only host context arrives after load as `window.researchData` and a `research-data` event: the research (id, code, name, goal), the entry (id, code, title, tags) and the section list. Render from it rather than hardcoding names.
- **Markdown exports do not contain the HTML.** A markdown export names the block and points at the web UI; a wall of markup in a `.md` file is neither readable nor markdown. The Obsidian vault export (`research_export` with `format: "obsidian"`) is the exception: there each `html` block is written as a real `.html` file under `_html/` and linked from the note. `research_export` / `research_import` carry `entry_type`, so the document survives a round trip. See [Export](/llms/export.md).

`[[E3]]` inside HTML is stored as literal text: cross-references are extracted from markdown content and from block text fields, never from a `code`, `mermaid` or `html` body.

### Statuses

| Status | Meaning | When to use |
|--------|---------|-------------|
| `draft` | Work in progress (default) | Initial creation, incomplete content |
| `active` | Published and current | Entry is complete and represents current understanding |
| `completed` | Finalized, no further changes expected | Section is being closed out |
| `archived` | Historical, superseded by newer entries | Content is outdated but preserved for reference |

### Content patterns

Entries should be self-contained markdown documents. Common patterns:

- **Finding**: synthesized answer to a research question with supporting evidence
- **Comparison**: table-based analysis of alternatives (tools, frameworks, approaches)
- **Guide**: step-by-step instructions derived from research
- **Summary**: high-level overview of a topic area with links to detailed entries via `[[E1]]`
- **Reference**: collected facts, specs, or configuration details

### Auto-generated fields

When `title` is null or empty, the server extracts it from the first non-empty line of content (stripping markdown heading markers, max 100 chars; `Untitled` if the content is blank).

When `description` is null, the server generates it from lines 2-5 of content (stripping markdown, max 200 chars).

For a `blocks` entry both come from the document instead: the title from the first `heading`, else — whichever comes first in the document — a `paragraph`'s opening sentence or an `html` or `transcript` block's `title`, and the call **fails** when the document offers none of those. The description falls back to the first paragraph that is not already the title, then to an `html` block's `caption`, a `task_ref` note, or a transcript's opening line.

When `session_id` is null, the server links the entry to the research's currently active session if one exists. Pass an explicit session ID to override that, and use `entry_update` with `session_id: ""` to unlink.

This means only `research_id`, `section_id`, and `content` need real values — everything else can be `null` and is inferred.

## Roadmap Node Types

Roadmap nodes have a `node_type` field that determines their visual appearance and semantic meaning:

| Type | Purpose | When to use |
|------|---------|-------------|
| `step` | Regular action item (default) | Most nodes — learning steps, tasks, stages |
| `milestone` | Key achievement or checkpoint | Mark significant completions or phase transitions |
| `decision` | Fork in the path | When the path branches based on a choice |
| `info` | Reference material or prerequisite | Context that isn't an action step |
| `group` | Container for related steps | Visual grouping of related nodes |
| `checklist` | Sub-items with checkboxes | Use `metadata` JSON for items list |
| `note` | Free-form annotation | Sticky-note style commentary on the graph |
| `link` | External URL reference | Use `metadata` JSON for the URL |
| `metric` | KPI or numeric indicator | Use `metadata` JSON for the value |

### Entity references on nodes

Nodes can reference existing research entities via `ref_type` and `ref_id`. Referenced data is resolved at read time (always shows current state):

| ref_type | References | Synced data |
|----------|-----------|-------------|
| `entry` | An entry | Title, status, content preview, section |
| `task` | A task | Title, status, priority, result |
| `session` | A session | Title, status, question progress |
| `research` | Another research | Name, status, section/entry counts |
| `question` | A question | Text, status, answer |

## Common Pitfalls

### 1. Sending empty values for fields that need content

Beyond schema validation, each create tool checks a few fields for non-empty values and returns `isError: true` with a list of everything missing — read the message, it names each field.

**Fields that must carry a real value:**

| Tool | Must be non-empty |
|------|-------------------|
| `research_create` | `name` |
| `entry_create` | `research_id`, `section_id`, `content` |
| `session_create` | `research_id`, `title` |
| `question_create` | `session_id`, at least one question with `text` |
| `task_create` | `research_id`, `title` |
| `roadmap_create` | `research_id`, `title` |
| `annotation_list` | `research_id` |
| `annotation_answer` | `annotation_id`, `resolution` — a mark moved to `answered` with nothing recorded cannot be reviewed, so an empty one is refused |
| `skill_load`, `skill_list`, `skill_attach`, `skill_detach`, `skill_copy`, `skill_promote`, `skill_fork` | `research_id` (`slug` too, except `skill_list`; `skill_load` and `skill_list` take an id form instead) |
| `skill_create` | `name`, `description`, `body`, and exactly one of `research_id` / `team_id` |
| `skill_update`, `skill_delete` | `skill_id`, **or** `research_id` with `slug` |

### 2. Confusing tool errors with protocol errors

MCP tools in this server **never return Go errors** (protocol-level errors). All failures are returned as `CallToolResult` with `isError: true` and a descriptive text message. If you get a protocol error (like `-32602: invalid params`), the input schema rejected your arguments before the tool code ran. The two usual causes: a property was left out (all properties are required — see Nullable and Optional Fields), or `null` was sent into a plain string/integer parameter.

### 3. Forgetting temp_id in roadmap creation

When creating roadmaps, edges reference nodes by `temp_id`. If you forget to set `temp_id` on a node that an edge references, the edge will have a broken connection.

```json
{
  "nodes": [
    { "temp_id": "n1", "title": "Step A" },
    { "temp_id": "n2", "title": "Step B" }
  ],
  "edges": [
    { "source": "n1", "target": "n2" }
  ]
}
```

### 4. Not setting result when completing tasks

When changing a task status to `completed` or `failed`, always set the `result` field in the same call. The result is the permanent record of what happened.

### 5. Sending answers without setting status

When answering a question, set both `answer` and `status: "answered"` in the same `question_update` call. Setting status to `answered` without an answer will fail validation.

### 6. Combining replace and append parameters

`research_update` no longer accepts `instruction` or whole-array `memory`. Use private skills for rules, `add_memory` for one new note, or `research_memory` to edit/delete explicit IDs. Notes expose `id`, `text`, `created_at`, `author`, `version` and optional session provenance. Pass the real research session in `session_id`; never infer it from whichever session was most recently active. For `session_update`, `notes` and `add_note` remain mutually exclusive.

`research_memory` requires `research_id` (UUID or R code) and `action`. Send
only the additional fields needed for the action:

```json
{"research_id":"R1","action":"list"}
{"research_id":"R1","action":"add","text":"Customer interviews changed our initial assumption.","session_id":"SS1"}
{"research_id":"R1","action":"update","item_id":"item-uuid","text":"Corrected finding.","version":1}
{"research_id":"R1","action":"delete","ids":["item-uuid"]}
```

Add/update text must be nonblank and at most 64,000 bytes. Delete accepts
1–500 explicit IDs. List returns an array of items; add returns the new item;
update/delete return `{"updated":true}`. After an edit, list or `research_get`
provides the new version for the next edit. A stale version fails with a
conflict: reload and reconcile the text before retrying. An omitted session
records no session provenance. Authors are stamped by the server (`agent` or
`user`); imported legacy notes have `unknown` authors and null creation times.

### 7. Editing a blocks entry as if it were text

`text_replace` is rejected on an entry with `entry_type: blocks` — it would rewrite the stored JSON as a string. Use `entry_patch` for part of the document, or `entry_update` with the whole document in `content`. Switching an entry **to** `blocks` also needs the block-form `content` in the same call; without it the call fails rather than wrapping the markdown in one paragraph.

### 8. Rewriting an entry without reading what happened to it

An entry may have been written by another session, corrected by a person, or
already fixed by you. `entry_history` costs one call and tells you who last
touched it and what they changed; `entry_diff` shows the change itself. Nothing
is lost if you overwrite it — every write that changes something appends a
revision, and an earlier one can be restored — but undoing someone's correction
and not noticing is a real failure mode, and the history is how you avoid it.

Three more things worth knowing about revisions:

- A write that changes nothing appends nothing, so re-sending identical content
  leaves the history as it was.
- `entry_history` returns no content — it is metadata per revision. Use
  `entry_diff` for what changed and `entry_read` for the current text.
- A `rev` (the 12-character content hash a `blocks` entry carries for optimistic
  concurrency, returned by `entry_read` and `entry_patch`) is **not** a revision
  number. Never pass one into `from` / `to`.

See [Revisions](/llms/revisions.md).

### 9. Assuming every research you can see is one you can write to

Reading a research is not permission to change it. Check `research_list` for
`access: "read-only"`, or `role` on the record `research_get` returns, before you
plan a session, an entry or a task against a research you did not create. A
`viewer` gets `your role in this team does not allow this` on the first write —
after the user has already answered your questions. See
[Access](#access-you-can-see-more-than-you-can-write).

### 10. Filling a metadata field you do not actually know

This is the failure mode with no human equivalent. A person leaves a field blank
when they do not know; a model fills it confidently and plausibly — `owner:
platform-team`, because the document mentions a platform. Blank used to mean
"unknown", and with an agent author there are no blanks.

**Send `null` as the value.** It records an explicit unknown, it answers a
required field, and it is the only thing that stops a required field from
manufacturing a fact. Omitting the key says nobody recorded anything; `null` says
somebody looked and could not say.

Two more habits worth having:

- **Read `metadata_report` on the response.** A key the section does not declare
  is dropped, and the entry comes back looking fine. The report is the only place
  the loss is stated.
- **Call `section_list` before writing into a section you have not written into
  before**, and read the existing documents. The first two or three entries in a
  section set the pattern for every one after, and the `help` line on a field
  says where its value is supposed to come from.
- **The same payload may carry an `instruction`** — the section's own rule for
  how a document here is written. Follow it for this document; it beats the
  research's memory and any skill on that narrow question, and it does not reach
  beyond it.

See [Document Metadata](/llms/metadata.md).

### 11. Trying to finish an annotation, or retrying the refusal

`annotation_answer` is the whole of your reach: it sets `answered` and stops
there. There is no tool argument, and no REST body an agent credential can send,
that reaches `closed` or `dismissed` — `only a person may close or dismiss an
annotation` is a decision, not a transient failure, and retrying it or looking
for another route is wasted work. Report the mark as answered and say what you
did.

Two neighbouring refusals read differently and mean different things:

- `annotation … is already settled` — somebody closed or dismissed it while you
  were working. Do not re-open it; move on to the next mark.
- `your role in this team does not allow this` — you are a `viewer` here.
  Answering a mark is a write, exactly like writing an entry.

And a rewrite is not an answer. Editing the paragraph a `disagree` mark points at
until the objection no longer applies destroys the disagreement instead of
recording it; the mark stays open and the position it defended is gone. See
[Annotations](/llms/annotations.md).

### 12. Deleting because somebody said "tidy up"

The delete tools have no undo, and the sentences that most often precede a
mistaken call are the vague ones: "clean this up", "we don't need the old
sections", "get rid of that". None of them names a record. Ask which, and prefer
the reversible move — `archived` on a research, `completed` on a session,
`skipped` on a question — unless the person asked for the thing to be **gone**.

Two specific traps:

- **`confirm: true` on `research_delete` is not a formality to satisfy.** It is
  the one place the product asks whether a person actually said this. Setting it
  because the schema wanted a value is the failure this guard exists for.
- **`force: true` on `section_delete` is not "retry harder".** The refusal is
  telling you how many documents would go. Report that number and wait for an
  answer; do not re-send with `force` because the first call failed.

See [Deleting Is Permanent](#deleting-is-permanent).

## Short Codes

Every entity gets an auto-assigned short code on creation. These codes can be used in URLs and cross-references instead of UUIDs:

| Entity | Pattern | Scope | URL example |
|--------|---------|-------|-------------|
| Research | `R1`, `R2` | Global | `/research/R1` |
| Section | `S1`, `S2` | Per research | — |
| Entry | `E1`, `E2` | Per research | `/research/R1/entry/E2` |
| Session | `SS1`, `SS2` | Per research | `/research/R1/session/SS1` |
| Question | `Q1`, `Q2` | Per session | `/research/R1/session/SS1/question/Q1` |
| Task | `T1`, `T2` | Per research | — |
| Roadmap | `RM1`, `RM2` | Per research | `/research/R1/roadmap/RM1` |
| Node | `N1`, `N2` | Per roadmap | — |
| Annotation | `A1`, `A2` | Per research | `/research/R1/annotations` (the queue page; a mark has no URL of its own) |

`[[A4]]` is **not** a cross-reference. Only `E`, `R`, `RM` and `RM:N` resolve, so a mark named in prose stores an unresolved reference and renders as inert text. Name what you produced instead — `[[E19]]`, `[[Q7]]` — which is what `annotation_answer` indexes.

Which tools hand codes back:

- `research_create`, `research_import`, `research_get` — research code
- `entry_create`, `entry_list`, `entry_read`, `entry_patch` — entry codes (`entry_read` returns the whole entry record, so `code` is on it)
- `roadmap_create`, `roadmap_list`, `roadmap_get`, `roadmap_update_node` — roadmap and node codes
- `session_get` — session code (inside the `session` object)
- `annotation_list`, `annotation_answer` — the mark's `code` (`A4`) and the `entry_code` it sits in, so a report to the user can name both
- `research_resume` — the only tool that hands back task (`T4`) and question (`Q7`) codes, plus session (`SS2`), entry (`E2`) and annotation (`A4`) codes, on every item it lists and inside `next_actions.target`. Each item carries its UUID beside the code, so the follow-up call has what it needs
- Section codes are **not** returned by any tool: `research_get` and `section_list` return section UUIDs only, and `question_list`, `session_get` questions, `task_list` and `task_create` return UUIDs without codes as well. Use those UUIDs in subsequent tool calls, and the REST API (`GET /api/researches/{id}`, `/tasks`, `/sessions/{sessionId}`) if you need the codes themselves.

Where codes are accepted as tool input:

| Accepts UUID **or** code | Accepts UUID only |
|--------------------------|-------------------|
| `research_get`, `research_update`, `research_memory`, `research_export`, `research_resume`, `research_delete`, `research_delete_preview` (`research_id`), every `skill_*` tool (`research_id`), `session_get`, `research_resume`, `research_update` and `research_memory` (`session_id` — an `SS` code is resolved inside the research you named, and one from another research reads as `not found`), `roadmap_get` (`roadmap_id`) | every other tool — `research_id` in `entry_create` / `entry_list` / `section_list` / `session_create` / `task_create` / `task_list` / `roadmap_create` / `roadmap_list` / **`annotation_list`**, `entry_id`, `question_id`, `task_id`, `annotation_id`, `section_id` in `section_delete`, `session_id` in `session_update` and `session_delete`, `roadmap_id` in `roadmap_update` / `roadmap_delete` / `roadmap_add_nodes` / `roadmap_remove_nodes`, `node_id` |

So keep the UUIDs returned by create calls. Short codes are for humans, URLs, and `[[...]]` cross-references — REST routes resolve them in `{id}` / `{sessionId}` / `{entryId}` / `{roadmapId}` path segments, MCP tools mostly do not. The exception on the REST side is the new delete routes: `DELETE /api/researches/{id}` takes an `R` code, but `DELETE /api/sections/{sectionId}`, `DELETE /api/sessions/{id}` and `DELETE /api/questions/{questionId}` want the UUID, exactly as their tools do.
