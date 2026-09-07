# Dovod

**A self-hosted workspace for research and decisions with AI.**

Dovod gives you and your AI assistant a shared project: evidence, documents,
questions, and next steps that stay available between conversations. Your
assistant structures the work and records what it finds. You review the
documents, challenge specific claims, and decide what to accept.

Use it to compare technologies, understand customers, investigate an incident,
or work out what to build next. The reasoning stays with the project, ready to
share with a teammate or continue in a new AI session.

Your assistant connects through the **Model Context Protocol (MCP)**. You work
in a **web interface** that updates as it writes. Dovod runs as one Go binary
with an embedded UI and stores your projects in SQLite, PostgreSQL, or MySQL.
You bring the AI client and model; Dovod provides the workspace and the tools
to work on it.

[Get started](#get-started) · [Connect your AI](#connect-your-ai) ·
[Methodologies](internal/docs/templates.md) · [Configuration](#configuration) ·
[API and documentation](#api-and-documentation)

## When to use Dovod

Use Dovod when a question needs investigation, review, and a record you can
return to. It fits the work of engineers, product teams, founders, and
researchers who already use an AI assistant.

| You are working on | Keep in the project |
| --- | --- |
| A technology or vendor choice | Criteria, source material, tradeoffs, and the reason for the choice |
| Customer or market discovery | Interview questions, answers, findings, and the evidence behind them |
| An incident or system investigation | A timeline, competing explanations, open questions, and follow-up tasks |
| Product priorities | Candidate features, constraints, decisions, and a roadmap |
| A literature review | Sources, linked findings, disagreements, and gaps to investigate |

## How it works

1. **Start with a question.** Tell your connected assistant what you need to
   understand or decide. Choose a methodology, or let it help you find one.
2. **Build the project together.** The assistant asks questions, records your
   answers, and writes documents with cross-references, diagrams, and tasks.
   You can follow the work in the browser as it happens.
3. **Review the reasoning.** Select a sentence and mark it **Verify**, **Dig**,
   or **Disagree**. The assistant reads the marks and answers them. You accept
   the response or send it back for more work.
4. **Continue with context.** In a new chat, ask to continue the project by its
   short code, such as `R1`. The assistant can load the project, outstanding
   tasks, open questions, and marks waiting for review.

For example, send this to an assistant connected to Dovod:

```text
Use the research/initialize prompt to start a project in Dovod.
We need to choose a search engine for our support documentation.
Help me define the criteria before comparing candidates, keep sources
with the findings, and record the final decision and its tradeoffs.
```

Later, after reviewing its work:

```text
Continue R1. Read the project context and continuation summary.
Review my marks on the documents and propose the next step.
```

The AI runs in your connected client. Dovod keeps the project available when
that conversation ends; work continues when you ask an assistant to resume it.

## What stays in your project

- **Documents and evidence.** Markdown and structured blocks, tables,
  checklists, transcripts, Mermaid diagrams, and sandboxed HTML artifacts.
  Cross-references such as `[[E3]]` connect documents; `[[R2:E5]]` links across
  projects.
- **Questions and answers.** Interview sessions with follow-up questions,
  deferred answers, and a visible record of what remains open.
- **Review and history.** Marks on specific passages, the assistant's
  responses, revision diffs, and the ability to restore an earlier version.
  Each reader has their own queue of new and changed documents.
- **Tasks and plans.** A task board, roadmaps, a project mind map, and a
  knowledge graph of cross-references.
- **Context for later work.** Private skills, memory, per-section writing
  instructions, reusable methodology, and a continuation summary that points
  to unfinished work.
- **Archiving and deletion.** Archive a finished project to put it out of the
  way and restore it later, or delete one outright — a project, a section, a
  document, a session, or a question. Deletion is real: no trash and no restore
  window, because a hidden copy of work you asked to be rid of is not data you
  own. Deleting a project is the owner's decision, asks them to type its short
  code, and offers a copy to download first; an assistant is instructed to
  delete nothing you did not name, and needs an explicit confirmation to delete
  a project at all. References from *other* projects keep the text that was
  written and stop resolving — removing them would edit a project nobody asked
  to change.

### Start with a methodology

Built-in methodologies cover technology comparison, user interviews,
competitive analysis, incident postmortems, roadmap prioritisation, and more.
They guide the assistant through the questions and decisions that matter for
that kind of work.

Open **Methodologies** in the web UI, choose a guide, and click **Copy prompt**.
Paste it into your connected assistant. The prompt includes your server's
`/llms.txt` address and the selected methodology. Teams can adapt methodologies
and attach reusable skills for work such as interviewing or grading evidence.

See the [methodology catalogue](internal/docs/templates.md) and
[skills guide](internal/docs/skills.md).

A rule about how to work belongs in the most specific place that fits, and the
most specific one wins: the project's memory says what *this project* is, a
skill says how a *kind of work* is done, and a section's instruction says how
to write a document *in that section*. Project-specific rules live in private
skills; reusable methodology lives in team or built-in skills. A section
instruction is a few imperatives, capped at 500 characters, that the assistant
reads before every document it files there and you see above that section's
document list — add one in **Settings → Sections**, beside the fields that
section declares.

Legacy project-level `instruction` text is migrated losslessly to an attached
private skill marked for trigger review.

When upgrading an existing installation, take a database backup first. The
migration replaces legacy memory and instruction columns; reverting to an older
binary requires restoring that backup. API clients must use structured memory
items: append with `add_memory`, edit/delete by item ID with `research_memory`
or the REST memory routes. Whole-array `memory` writes and project-level
`instruction` writes are rejected; a section's instruction is a separate field,
written with `section_update` or `PUT /api/sections/{id}`. Portable exports use
version 2; version 1 imports remain supported.
See the [database upgrade guide](docs/databases.md) for deployment and rollback steps.

### Share the result

Work with a team, send a read-only share link, or export a project as Markdown,
a printable document, an Obsidian vault, or portable JSON for another Dovod
instance. A share link opens the documents together with the knowledge graph
and the mind map. Links can expire, require a password, and be revoked. You
choose whether they also include sessions, tasks, roadmaps, and export, and you
can change that choice later on a link people already hold, without issuing a
new address.

Private skills, memory, section instructions, revision history, and review
marks stay out of public share links, and the shared graph and mind map leave
out whatever the link does not include.

## Get started

### Run the current version with Docker

This builds the current source, including the Dovod interface described above.
It requires Git and Docker.

```bash
git clone https://github.com/dovod-app/app.git
cd app
docker build -t dovod:local .

docker run -d --name dovod \
  -p 127.0.0.1:8088:8088 \
  -v dovod-data:/data \
  -e MCP_RESEARCH_DB=/data/dovod.db \
  -e MCP_RESEARCH_TRANSPORT=sse \
  -e MCP_RESEARCH_AUTH_ENABLED=true \
  -e MCP_RESEARCH_BASE_URL=http://localhost:8088 \
  dovod:local
```

Open **[localhost:8088](http://localhost:8088)**, create your account, and
[connect your AI assistant](#connect-your-ai). The `dovod-data` volume keeps
your projects across container restarts. This example exposes the web port on
your own machine; see [deployment](#deployment) for a shared server.

Keep `MCP_RESEARCH_AUTH_ENABLED=true`. A container is another machine as far as
Dovod is concerned: requests from your browser reach it over the Docker network,
not from its own loopback address, so without accounts or an `api_token` the
server accepts no writes and the interface is read-only. See
[who can write to your server](#who-can-write-to-your-server) for the other
ways to configure it.

The binary, environment variables, and API identifiers retain `mcp-research` /
`research` names for compatibility. The product and UI use **Dovod**,
**Projects**, and **Documents**.

### Use a published release

Download a binary for macOS, Linux, or Windows from
[Releases](https://github.com/dovod-app/app/releases/latest). The binary
includes the web interface. Published releases can lag behind `master`;
use the source build above for the current UI and features.

For example, on Linux x86_64:

```bash
curl -fL -o mcp-research \
  https://github.com/dovod-app/app/releases/latest/download/mcp-research-linux-amd64
chmod +x mcp-research
./mcp-research --transport sse --db dovod.db \
  --auth-enabled --base-url http://localhost:8088
```

Release assets also include `darwin-arm64`, `darwin-amd64`, `linux-arm64`,
`windows-amd64.exe`, and `windows-arm64.exe` builds. Container releases are
published as `ghcr.io/dovod-app/app:latest` and versioned tags.

Give SQLite a database path such as `--db dovod.db`. Without a path or DSN,
the default SQLite database is in memory and its contents disappear on exit.

## Connect your AI

### Connect to a running server

Add a **Streamable HTTP MCP server** in your AI client's MCP settings:

```text
http://localhost:8088/mcp
```

For a deployed instance, use `https://your-server/mcp`. An OAuth-capable client
can sign in with your Dovod account. If your client supports bearer headers,
create an API key in **Settings → API Keys** and send it as
`Authorization: Bearer <your-api-key>`.

The MCP endpoint takes the same credential as the write API. On a server with
only an `api_token` set, send that token as the bearer. On a server with
neither credential, MCP answers only clients on the machine it runs on — which
is the local case above, and never a deployed one.

The client must be able to reach that address. A hosted AI client needs a
reachable HTTPS deployment of Dovod.

Streamable HTTP uses the web port. Legacy SSE is also available at
`:8081/sse` when running with `--transport sse`; expose that port only if your
client uses it. It expects the same credential as a bearer header. An account
token may instead go in a `?token=` parameter, for clients that cannot set
headers; the instance `api_token` may not — a query string ends up in every
proxy log, and that token does not rotate.

### Let a local client start the binary

For an MCP client that launches a process over **stdio**, add a server entry
like this to its MCP configuration, using absolute paths:

```json
{
  "mcpServers": {
    "dovod": {
      "command": "/absolute/path/to/mcp-research",
      "args": [
        "--db", "/absolute/path/to/dovod.db",
        "--auth-enabled",
        "--default-user", "you@local.dev"
      ]
    }
  }
}
```

The client starts Dovod and its web UI together. `--default-user` creates the
local account if needed, runs stdio tools as that user, and signs the browser
in automatically. Use this mode on a trusted local machine. Give each running
instance its own web port with `--web-port` if another server is using 8088.

Once connected, use the example [start prompt](#how-it-works) or copy one from
**Methodologies** in the browser.

## Configuration

Settings are read in this order: **CLI flags → environment variables →
`config.yaml` → defaults**. See [config.yaml.example](config.yaml.example)
for a server configuration.

| Setting | CLI flag | Environment variable | Default |
| --- | --- | --- | --- |
| Transport | `--transport` | `MCP_RESEARCH_TRANSPORT` | `stdio` |
| Web / REST / HTTP MCP port | `--web-port` | — | `8088` |
| Legacy SSE port | `--mcp-port` | — | `8081` |
| Database driver | `--db-driver` | `MCP_RESEARCH_DB_DRIVER` | `sqlite` |
| Database DSN | `--db-dsn` | `MCP_RESEARCH_DB_DSN` | — |
| SQLite file | `--db` | `MCP_RESEARCH_DB` | In memory |
| Authentication | `--auth-enabled` | `MCP_RESEARCH_AUTH_ENABLED` | `false` |
| JWT signing secret | `--jwt-secret` | `MCP_RESEARCH_JWT_SECRET` | Generated on startup |
| Registration | `--allow-registration` | `MCP_RESEARCH_ALLOW_REGISTRATION` | `true` |
| Public URL | `--base-url` | `MCP_RESEARCH_BASE_URL` | — |
| Local default user | `--default-user` | `MCP_RESEARCH_DEFAULT_USER` | — |
| Operator API token | `--api-token` | `MCP_RESEARCH_API_TOKEN` | None — writes only from the server's own machine |
| Revision retention limit | `--revision-limit` | `MCP_RESEARCH_REVISION_LIMIT` | `0` — keep all |
| Log level | `--log-level` | `MCP_RESEARCH_LOG_LEVEL` | `info` |
| Config file | `--config` | `MCP_RESEARCH_CONFIG` | `./config.yaml` |

Set a persistent `jwt_secret` to keep login sessions valid across server
restarts. The `--default-user` convenience above is for local use; omit it on
shared deployments — the automatic browser sign-in is offered only to a browser
on the server's own machine.

### Who can write to your server

Without `auth_enabled`, anyone who can reach the address can read every project.
Who can *write* — through the web interface, the REST API, or any MCP tool —
depends on which credential you configured:

| Configured | Who can write |
| --- | --- |
| `auth_enabled` | Signed-in accounts, their API keys, and OAuth clients, subject to team role |
| `api_token` only | Any caller sending `Authorization: Bearer <token>`. A browser cannot hold one, so edits made in the web interface are refused |
| Neither | Only callers on the machine Dovod runs on. Anything else is refused with `401` |

The third row is the single-binary local mode, and it is exact: the request has
to come from the server's own loopback address and carry no proxy forwarding
header. A published container port, a reverse proxy, and another computer on
your network are all outside it. Dovod logs a warning at startup when it is
running that way, and `GET /api/health` reports `write_api` for the caller
asking, so you can check from wherever you are.

### PostgreSQL and MySQL

Create an empty database and provide its connection settings. Schema
migrations run when Dovod starts.

```bash
MCP_RESEARCH_DB_DRIVER=postgres \
MCP_RESEARCH_DB_DSN='postgres://user:password@localhost:5432/dovod?sslmode=require' \
./mcp-research --transport sse --auth-enabled

MCP_RESEARCH_DB_DRIVER=mysql \
MCP_RESEARCH_DB_DSN='user:password@tcp(localhost:3306)/dovod' \
./mcp-research --transport sse --auth-enabled
```

Changing the driver selects a different database; it does not move existing
data. See [database setup and testing](docs/databases.md).

## Deployment

**A server another machine can reach needs `auth_enabled` or an `api_token`.**
With neither, Dovod refuses every write that arrives over the network — and
behind a reverse proxy that is all of them — while leaving every project
readable to anyone who has the address.

For a shared instance, enable authentication, set a persistent JWT secret,
and set `base_url` to the public HTTPS address. Configure registration to suit
your team. The repository includes a Compose setup:

```bash
cp config.yaml.example config.yaml
# Edit base_url, jwt_secret, and registration settings in config.yaml.
docker compose up -d
```

Compose builds from source and stores SQLite data in the `mcp-data` volume;
`config.yaml.example` enables authentication, so keep that setting when you edit
your copy. The [nginx configuration](deploy/nginx/mcp-research.conf) shows how
to proxy the UI, MCP, OAuth, and WebSocket connections. Back up your database
and keep the server configuration with it.

Each account gets a personal team. Additional teams use these roles:

| Role | Access |
| --- | --- |
| Viewer | Read and export |
| Editor | Viewer access, plus creating, editing, and deleting project content |
| Owner | Editor access, plus managing members, moving projects between teams, and deleting the project itself |

People join through invite links. For readers who do not need an account, use
a revocable share link.

## API and documentation

Every running instance serves documentation for both people and AI clients:

| Address on your server | Use it for |
| --- | --- |
| `/llms.txt` | Give an assistant the entry point to Dovod's instructions |
| `/api-docs` | Browse the REST API and try requests against your instance |
| `/api/openapi.yaml` or `/api/openapi.json` | Get the generated OpenAPI specification |
| `/llms/mcp-client-guide.md` | Read tool conventions and integration details |

An assistant that can read URLs and make HTTP requests can use the REST API
with the appropriate credential. With authentication enabled, use a user API
key, session token, or OAuth token for project access. The separate instance
`api_token` is an operator credential, including for server-wide methodologies.
The API reference describes which credential each route accepts.

The same guides are available in this repository:

- [MCP client guide and tools](internal/docs/mcp-client-guide.md)
- [Project data model](internal/docs/domain-guide.md)
- [Review marks](internal/docs/annotations.md) and [revision history](internal/docs/revisions.md)
- [Block documents](internal/docs/blocks.md) and [HTML artifacts](internal/docs/artifacts.md)
- [Tasks](internal/docs/tasks.md), [roadmaps](internal/docs/roadmaps.md), and [exports](internal/docs/export.md)
- [Document metadata](internal/docs/metadata.md) — the fields a section declares, beside its instruction
- [Conducting a project](internal/docs/conducting-research.md) — the step-by-step guide an assistant follows

The API calls projects `research` and documents `entry`. Existing tool names,
routes, short codes, and integrations keep working with those identifiers.

## Build and contribute

Use Go 1.25+, Node.js 22, and npm 11. From the repository root:

```bash
npm install -g npm@11
make frontend-install
make build-all
make run-sse
```

`make build-all` generates the frontend, embeds it in the Go binary, and writes
`bin/mcp-research`. `make run-sse` starts it with a persistent `research.db`.

```bash
make test                   # Go tests, after preparing the embedded frontend
make frontend-dev           # Nuxt dev UI on :3000, using the API on :8088
make storybook              # Component catalogue
node frontend/scripts/css-consistency.mjs
```

The backend is Go with Bun for database access; the frontend is an embedded
Nuxt SPA. See [CLAUDE.md](CLAUDE.md) for architecture and contributor guidance.

### Host the project list alongside a website

The project list defaults to `/`. To serve a separate landing page at `/` and
Dovod projects at `/projects`, set the **build-time** frontend route:

```bash
docker build --build-arg NUXT_PROJECTS_PATH=/projects -t dovod:hosted .
# Or, when building the frontend directly:
cd frontend
NUXT_PROJECTS_PATH=/projects npm run generate
```

The setting changes only the project-list route. Project details, authentication,
API, OAuth, MCP, and asset paths stay at their existing addresses. Navigation,
breadcrumbs, the home shortcut, and login/register fallbacks resolve the named
project-list route. The generated web app manifest starts at that route too.
Changing a running container's environment is insufficient;
rebuild the frontend or image. The default build remains suitable for local use.

Use nginx to serve website routes explicitly and proxy the remaining paths to
Dovod without rewriting the URI. Give the website a different Nuxt asset prefix
(e.g. `/website-assets/`), leaving `/_nuxt/` to the application. Set
`MCP_RESEARCH_BASE_URL=https://dovod.app` and preserve the host, forwarded protocol,
WebSocket upgrade headers, and unbuffered streaming responses at the proxy.
The companion `dovod-app/website` repository includes this nginx configuration.

## License

MIT — see [LICENSE](LICENSE).
