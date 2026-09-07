# Templates

A template is a **kickoff methodology the model reads** — not a skeleton the code
clones.

It carries no sections, no questions and no rows. It carries a name, the criteria
an agent matches on, and a markdown body saying what to ask the person before
proposing anything, what structure to suggest, what a good entry looks like, and
when the research is finished. The model then designs the research itself.

> A template says how a kind of research is **started**. A skill says how a kind
> of work is **done**.

## Using one

```
template_list()     -> {templates: [{slug, name, tier, description, when_to_use, when_not_to_use}], usage_hint}
template_get(slug)  -> {slug, name, tier, when_to_use, when_not_to_use, body, skills, version, usage_hint}
research_create(..., template_slug: "technology-comparison")
```

`template_list` takes **no arguments** — send `{}`. It never carries a body:
twenty-five methodologies arriving in a kickoff that needs one is the cost this feature
exists to avoid. `template_get` takes the slug you read there (an id works too,
but you will not have one) and is the only call that returns a body.

Three rules the kickoff prompt states and this one repeats because they are the
whole point:

- **Ask what decision is waiting before you offer a template.** A structure shown
  first anchors the person to it — they recognise most of it, accept, and the
  parts that do not fit quietly redefine the work.
- **The body is instructions, not a form.** Follow it. Its section list is a
  starting point to adapt to the conversation, never a list to recite.
- **Create a section when you have something to put in it.** The conductor aims
  at the least-covered sections, so an empty one is a standing instruction to
  invent content for it.

Passing `template_slug` to `research_create` does the only structural thing in
the feature: it stamps `template_slug` and `template_version` on the research,
and attaches the [skills](/llms/skills.md) that methodology names. The version is
stamped because the built-ins are refreshed from the binary at every boot —
without it, an upgrade would silently change the text behind a research already
in flight.

The tool answers with `skills_attached` and, when the methodology named something
this research could not get, `skills_unavailable`. Neither key appears when it is
empty. A skill that could not be attached never fails the creation: a research
that exists with five of six skills beats a create call rolled back because a
template named something a later build removed. Read `skills_unavailable` and
tell the user which part of the methodology will not be backed by a skill.

Two things `template_slug` does **not** do. It does not create sections,
questions or tasks — you design those from the conversation. And it is an **MCP
argument only**: `POST /api/researches` has no `template_slug` field, so a
research created over REST starts with no provenance and no attached skills. The
provenance cannot be added afterwards; the skills can — `skill_attach` or
`POST /api/researches/{id}/skills` takes them up one at a time.

## What ships

Twenty-five, refreshed from the binary at every boot.

| Slug | Ends in | Skills it attaches |
|---|---|---|
| `technology-comparison` | a pick you can defend, closing on an ADR-shaped entry | `evidence-grading` |
| `competitive-landscape` | a recommendation with the two facts that would reverse it | `evidence-grading` |
| `user-interview-study` | findings traceable to what real people said | `structured-interviewing`, `evidence-grading` |
| `literature-review` | conclusions a reader could check, including what was excluded | `evidence-grading` |
| `financial-position` | actions with an owner, a date and a cash effect that reach a stated target | `evidence-grading` |
| `audience-definition` | one segment served first, at least two declined with the reason, and a trigger somebody could detect | `structured-interviewing`, `evidence-grading` |
| `monetisation-readiness` | a metric, what stays free, a price per package, and three real accounts' new bills | `evidence-grading` |
| `venture-discovery` | one venture with a named buyer and a channel, and the cheapest test that could kill it | `structured-interviewing`, `evidence-grading` |
| `startup-idea` | one thesis with a dated "why now", a buyer's existing workaround, and the strongest case against it | `structured-interviewing`, `evidence-grading` |
| `agent-economics` | a job, a cost per run, a price, and the error rate at which it stops being worth buying | `evidence-grading` |
| `personal-capital` | moves with a date, an hours cost and a downside the person's floor survives | `evidence-grading` |

**Building and product:**

| Slug | Ends in | Skills it attaches |
|---|---|---|
| `incident-postmortem` | a causal account responders sign, and at most five corrective actions that close | `structured-interviewing`, `evidence-grading` |
| `legacy-system-decision` | leave / contain / refactor / strangle / rewrite, with named doers and a tripwire | `structured-interviewing`, `evidence-grading` |
| `feature-discovery` | two or three candidates, each with its summoning evidence and cheapest kill test | `structured-interviewing`, `evidence-grading` |
| `feature-kill-or-keep` | kill with a date and migration, invest with a metric, or keep with the cost accepted in writing | `structured-interviewing`, `evidence-grading` |
| `roadmap-prioritisation` | an ordered plan with owners, explicit cuts, and criteria fixed before candidates were read | `structured-interviewing`, `evidence-grading` |

**Selling and growing:**

| Slug | Ends in | Skills it attaches |
|---|---|---|
| `channel-economics` | one channel under budget with a kill number, two declined with the arithmetic | `structured-interviewing`, `evidence-grading` |
| `retention-diagnosis` | one leak, who it takes, an intervention with a number — or an honest "the work is upstream" | `structured-interviewing`, `evidence-grading` |
| `churn-diagnosis` | at most three causes claiming named regretted accounts, plus the churn you keep | `structured-interviewing`, `evidence-grading` |
| `win-loss-analysis` | why deals are really won and lost, from the buyers, with changes owned and dated | `structured-interviewing`, `evidence-grading` |
| `live-deal-qualification` | pursue at a named cost or qualify out, on tests the buyer can refuse | `structured-interviewing`, `evidence-grading` |

**Capital and career:**

| Slug | Ends in | Skills it attaches |
|---|---|---|
| `raise-or-not` | raise / don't / raise-on-a-trigger, with the zero-raised plan as candidate zero | `evidence-grading` |
| `business-acquisition` | buy at X under conditions or walk — and one word: machine, job, or liability | `structured-interviewing`, `evidence-grading` |
| `stay-or-leave` | a dated decision outside any spike window, every complaint tagged and asked about | `evidence-grading` |
| `offer-negotiation` | a derived walk-away, a justified opening, and word-for-word responses to their moves | `evidence-grading` |

Each carries a correction to the obvious version of itself. They are the
difference between the template and a section list:

- **Comparison:** criteria are fixed and weighted *before* candidates are named,
  or they get retrofitted around a favourite. "Keep what we have" is candidate
  zero.
- **Landscape:** a company is a direct competitor only if a real buyer considered
  it in the same purchase. Everything else — the spreadsheet, doing nothing — is
  a substitute, and half of real scans find the threat there.
- **Interview study:** two people make a finding, one makes a signal. Every
  finding carries a verbatim quote and a participant id.
- **Literature review:** what was excluded and why is the load-bearing artifact.
  A list of what you kept is unfalsifiable.
- **Financial position:** never analyse an average while the underlying rows
  exist — a 41% blended margin is work at 60% plus work that lost money, and only
  the second is actionable. Dropping the loss-making customers shrinks the
  business unless the costs leave with them.
- **Audience:** a segment is a **trigger, not a type** — the event that puts
  somebody into it, not what they are. And you will find the segment your
  recruiting channel contains, so write down where every person came from before
  believing one.
- **Monetisation:** the billing metric is decided first, and it is not the
  easiest thing to meter. A price set too low never announces itself — too high
  produces objections and churn, too low produces cheerful customers and a
  company that dies slowly.
- **Venture discovery:** a candidate is a problem, a buyer **and a channel** —
  the third is the one that goes missing. And a model will generate a hundred
  plausible ventures, none of them yours: what it generates is the average of
  what has been written about, which is the set with the most competition and the
  least of your unfair advantage. Generate to *attack* a candidate, never to
  produce one.
- **Agent economics:** the unit is a job somebody already pays for, never a
  capability, and the demo measures your taste in examples — run it on inputs you
  did not choose. The whole business lives in the fraction it gets wrong, and the
  worst failures are the ones the customer cannot detect.
- **Startup idea:** you are looking for a **change** that has not been priced in
  yet, not for an idea — a candidate whose "why now" is *"AI is getting better"*
  has not answered the question. Every candidate records why smart people think
  it is a bad idea, and whether they are wrong for a nameable reason; without one
  there is no secret and the race is decided on money. And building got cheap, so
  *"I could build that"* stopped being information — the survivors of that filter
  are the ideas everybody with your skills is having this month.
- **Personal capital:** income that stops when you stop is not capital, so the
  two columns are never summed; every move carries the **hours** it costs, which
  is what makes this different from a company's version. The best-sounding move
  is the one that got written about because it worked.
- **Postmortem:** "root cause" is a decision, not a discovery — the why-chain has
  no natural end, and organisations stop at the cheapest fixable point or the most
  junior person present. Contributing factors, plural, by construction.
- **Legacy system:** the old system is the only complete spec you have, and the
  comparison is rigged by memory — the imagined system on its best day against the
  real one on its worst. Leave-it-alone is candidate zero, written seriously.
- **Feature discovery:** users answer with solutions — record the problem they
  were solving. And every evidence channel was built to hear the engaged, so check
  the shortlist against the churn evidence too.
- **Kill or keep:** "keep" pays the same evidence bill as "kill", and "keep and
  watch" is not an outcome — date it and number it, or admit you chose keep.
- **Roadmap:** criteria are fixed and *ordered* before any candidate is read, or
  the matrix launders a decision the off-spreadsheet negotiation already made. A
  plan every stakeholder likes was ordered by seniority.
- **Channels:** the channel that worked is degrading while you scale it — record
  CAC by spend tranche, never cumulatively; the cumulative average hides the slope.
- **Retention:** an "improvement" is indistinguishable from an acquisition-mix
  change until cohorts and sources are held apart.
- **Churn:** the ex-customers who answer are the polite churn; test every
  interview-built cause against the trail of the silent ones.
- **Win/loss:** the buyer's stated reason is where the interview starts — "price"
  is a symptom until you know what they bought instead. Wins get the same
  scrutiny; a win with no rejected alternative is a renewal risk.
- **Deal qualification:** a champion is proven by what they do without you in the
  room; a test the buyer cannot refuse is not a test.
- **Raise or not:** almost everyone you can consult is paid in the "raise" answer,
  and the reference class is rigged twice — raised-and-died and never-raised-and-won
  are both invisible.
- **Acquisition:** after months inside a deal every problem becomes a discount
  instead of a signal — write the walk-away conditions before the verification
  work, as conditions, not a price.
- **Stay or leave:** you decide in a spike and gather evidence in the flat — date
  the trigger behind every complaint, and re-read the stay case written in
  different weather.
- **Negotiation:** by the time the offer arrives you have already moved in — the
  walk-away is derived on paper before the process makes you want the job.

## Two tiers, and two kinds of global

| Tier | `source` | Owner | Lifecycle |
|---|---|---|---|
| `global` | `builtin` | nobody | ships with the binary, refreshed at boot; **editable by no one**, including the operator — the next boot would undo the edit. Fork it |
| `global` | `user` | the operator of the instance | written through `POST /api/templates`, never touched by the refresh, edited and deleted by the operator |
| `team` | `user` | a team | written by that team, theirs alone |

**A team still cannot publish server-wide.** That refusal has not moved: a
template body steers a model, and lending one team's instructions to another
team's kickoff needs a trust story this product does not have. What exists is
narrower — whoever *runs* the server can add to the shipped set without shipping
a new binary, and they are outside that argument because they already own the
binary and the database.

The credential is the `api_token` from the config, and it is the only one that
works. Not a role: no role in this product grants it, a team `owner` is refused
with `operator_required`, and nobody can be promoted into it. With neither
`api_token` nor `auth_enabled` there is no boundary to prove anything across and
every caller **on the server's own machine** is treated as the operator; a
caller from anywhere else is refused before the route runs, exactly as every
other write is in that mode.

`source` is what keeps the two apart at boot, and it is load-bearing rather than
informational. The refresh matches on **slug *and* `source='builtin'`**, so a
global template written here is invisible to it; had it matched on the tier
alone, the first release shipping a file under the same slug would have
overwritten the operator's text with ours. As it is, the insert collides with the
unique index instead, that one file is reported as a problem and skipped, the
rest still load, and the operator's row survives untouched.

A team's fork keeps its parent's slug — it is the same methodology, edited — and
shadows the global one in every list that team sees, so the same slug is never
offered twice. Resolution follows the same order: a slug is looked up in your
teams' libraries first and falls back to the global set, which is why forking has
any effect at all. What you can see is taken from your own memberships, never
from anything you send, so a slug can never resolve into a team you are not in.
With `auth_enabled: false` there is no caller to scope to and every template on
the instance is visible. Writing a **team** one in that mode still needs a team
id, and `GET /api/teams` refuses without a signed-in user — so use the id the
local instance actually uses: **`team-local`**.
`POST /api/teams/team-local/templates` works and the template lands in the team
tier, not the global one. `POST /api/templates` works too and needs no
credential there, because with no `api_token` either there is no boundary to
prove anything across — but only from the machine the server runs on. Both of
those writes, like every other one in that mode, answer a caller from anywhere
else with `401 the write API is disabled: set api_token or auth_enabled to
accept writes from another machine`.

With accounts on, the **operator's own view is narrower, not wider**. A caller
presenting the `api_token` to `GET /api/templates` or `GET /api/templates/{slug}`
sees the global tier and nothing else — a team's template is `404` even by id.
The token proves who runs the server, never membership of a team, and it must not
become a way to read every team's private methodologies. It is still enough for
what it exists for: holding the id of a global, to edit or delete it. (The read
routes are otherwise unchanged, so on an instance with no accounts everyone,
operator included, still sees everything on it.)

## Writing one

**`when_to_use` is required and is what an agent matches on**, before it has read
anything else. Name the situation and the outcome: *"Use when a decision is being
made between named tools and the reasoning will have to survive review."* Both
criteria are capped at 240 characters, counted in runes.

**`when_not_to_use` is worth as much.** Knowing when a methodology is wrong is
what stops it being applied to everything.

**The body** is markdown, up to 24000 characters. It is read once, in full, by a
model that has just started — so unlike a skill description it can be a real
document. Give it four parts: what to ask before proposing anything, the
structure to propose, the working rules, and when it is done.

**`skills`** names the methodology [skills](/llms/skills.md) a research started
this way should follow. Slugs only, resolved as the owning team's copy if there
is one and the built-in otherwise. A *built-in* template naming a skill that does
not exist is **refused at boot** — that one file does not load, the rest do, and
every problem in the set is logged together; the server still starts, so a
missing methodology is a thing to look for in the startup log rather than a
crash. A template written here is not checked that way: neither a team's nor a
global one is refused for it. A slug that resolves to nothing comes back from the
write as a `warnings` entry, and on a single read as `skills_resolved[].missing`
rather than being dropped from the list.

**`name` is required and derives the slug**; `description` is one line for a
picker and is optional. An edit that restates only some fields inherits the rest
from the row it is editing rather than blanking them — restating a body must not
silently drop the line an agent matches on.

## Saving one from a research

`GET /api/researches/{id}/templates/draft` returns a **skeleton, not a capture**,
and creates nothing. A template carries no rows, so there is nothing to copy:
what comes back is `{data: {name, when_to_use, body, skills}, hint}` — the
sections that research actually grew, the non-ambient skills it follows, and
headings with the judgement left blank (*Before you propose anything*,
*Structure to propose*, *What good looks like here*, *When it is done*). The
`when_to_use` it returns is a placeholder that says to rewrite it. Somebody
writes the methodology into it and posts it to `/api/teams/{id}/templates`.

Anything else would be dishonest about what a template is.

## Sharing

A share link exposes no template list and no body. `TemplateService` refuses a
share context before it resolves anything, and there are no template routes under
`/api/shared/{token}/`, so a route added later still fails closed. Which
methodology a team follows is working process, the same class as memory and skills.

The stamp goes too. `redactForShare` blanks `template_slug` and
`template_version` alongside `memory` and the team fields, on the
one read path every reader of a research goes through — a slug is a name a team
chose, and "acme-q4-layoff-diligence" is not something a read-only link should
teach.

## REST

| Method | Path | Does |
|---|---|---|
| `GET` | `/api/templates` | the global set plus your teams', forks shadowing their parents, **no bodies**. `{data: [...]}`, each with `source` and derived `research_count` and `body_words`. Called with the `api_token` it returns the global tier alone |
| `GET` | `/api/templates/{slug}` | one, **with its body**. The path value is tried as an **id first, then as a slug** — an id is not a way past team scoping, so a template of a team you are not in is `404` either way. Also carries `skills_resolved`. Accepts the `api_token`, and then only a global one resolves |
| `POST` | `/api/templates` | write a **global** one, visible to every team on the instance. **Operator only** — `Authorization: Bearer <api_token>`. A signed-in caller who is not the operator gets `403 operator_required`; no credential at all is the ordinary `401`. Takes no `team_id` — sending one is a `400` on that field rather than a global template somebody meant to write for their team. `201 {data}`, with `warnings` when it names a skill that resolves to nothing |
| `GET` | `/api/teams/{id}/templates` | that team's own library, without the global set mixed in |
| `POST` | `/api/teams/{id}/templates` | write one for a team. `201 {data}`, with the same `warnings` when it names a skill that resolves to nothing |
| `POST` | `/api/templates/{slug}/fork` | copy a **global** into a team and apply the edit; `team_id` goes in the body. `200 {data, forked: true}` — follow the slug to the new row. Works on an operator-written global as well as a shipped one. A slug that is not a global one is `404` |
| `PUT` | `/api/templates/{templateId}` | edit in place; bumps `version`. A team template needs `editor` in that team; a global one needs the `api_token`. One that **ships with the app** is `not_allowed` for everybody — editing what ships is the fork route |
| `DELETE` | `/api/templates/{templateId}` | delete. `204`. Same three cases as `PUT` |
| `GET` | `/api/researches/{id}/templates/draft` | the skeleton above. `{id}` accepts a research short code |

Read by slug, write by id — the same split skills use, because a fork keeps its
parent's slug and a slug is therefore an address only within one caller's scope.

A write to a **team's** template asks that team for `editor` or better, since
there is no research to ask about: a non-member gets `404`, a member who is only
a `viewer` gets `403`. A write to a **global** one asks for the `api_token`
instead — no team is consulted, because the operator is in nobody's member list.
Fork is the exception that reads one and writes the other: it needs `editor` in
the team named by `team_id`, so with accounts on the `api_token` is not a
credential for it and gets the ordinary `401`. With `auth_enabled: false` there
is nobody to check and every route is permitted — to a caller the server accepts
a write from at all, which with no `api_token` configured means one on its own
machine.

| `code` | Status | Means |
|---|---|---|
| `slug_taken` | 409 | a template of that name already exists in the same tier — including forking a global twice into one team, and a global name already taken by one we ship |
| `not_allowed` | 403 | writing to or deleting a template that ships with the app. **Fork it instead** |
| `operator_required` | 403 | writing to the server-wide library without the instance `api_token`. There is no role that would fix this — create it in a team, or authenticate as the operator |

The two 403s are deliberately different codes. `not_allowed` means *fork it*;
`operator_required` means *you are not the person who may do this at all*, and a
client that cannot tell them apart offers the wrong next step.

Validation failures are `400` and carry a `field`: `name` (empty), `body` (empty,
or over 24000 runes), `when_to_use` (empty, or either criterion over 240 runes —
the error is reported against `when_to_use` even when `when_not_to_use` is the
long one), and `team_id` — required by fork, refused by `POST /api/templates`,
which writes for the whole instance and would otherwise answer `201` with a
server-wide template to somebody who plainly meant to write one for a team.

The web UI is **read-only**. `/templates` lists every methodology the reader can
see, grouped by where it came from — their teams', added on this server, ships
with the app — and `/templates/{id}` shows one with its body and the skills it
attaches. Nothing there writes, and no MCP tool does either: every create, fork,
edit and delete above is REST. Send a person to `/templates` to read one; send
them to a client or a terminal to write one.
