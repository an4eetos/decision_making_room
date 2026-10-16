# The campaign

The campaign is a view of what you already wrote, not a second app to maintain.
It has three kinds of item, on fronts you create:

| Item | On the map | Resolved as |
|---|---|---|
| **Objective** | a position to take: a goal | *taken*, or *withdrawn* — an orderly withdrawal is a legitimate order |
| **Obstacle** | opposition dug in between you and an objective | *cleared* |
| **Unknown** | fog of war: a question whose answer would change what you do | *lifted*, with the answer |

Open loops become **orders** when you aim them at an item. An order aimed at an
unknown is **reconnaissance**.

## Rules it keeps

- **Nothing goes on the map without your agreement.** Everything extracted is a
  proposal you keep or drop, the same rule as open loops. Editing a proposal,
  or filing it under a front, keeps it.
- **Fronts are yours.** Extraction never invents one. It files a proposal under
  a front you already have when the model names it exactly, and leaves it
  unassigned otherwise. An empty campaign offers one-click starters (Work,
  Health, Money, People).
- **Intelligence is labelled.** An obstacle's strength — 1 outpost, 2 dug in,
  3 fortress — is marked `est` until you confirm it or set it yourself.
- **Fog lifts only with an answer.** Lifting an unknown requires saying what
  reconnaissance found; an unknown closed without one has only been forgotten.
- **Every layer degrades to v1.** If you never open the page, nothing about
  answers changes.

## Opposition: the five kinds of stuck

Obstacles are typed by the five kinds of stuck from the Stalled mode, because
they need opposite treatments:

| Kind | Treatment |
|---|---|
| `undefined` — the next step is not defined | define the next step |
| `waiting` — waiting on input not chased | chase the input |
| `fear` — afraid of what the result will show | look at the result |
| `too_big` — too large to hold in one sitting | cut it to something that fits |
| `unwanted` — you do not actually want the outcome | consider withdrawing the objective |

## Where proposals come from

**Ordinary turns** cost one background model call, after the answer is sent,
and only when a free phrase prefilter sees goal, blocker or uncertainty language
in what you wrote. The model gets your fronts, your active objectives numbered,
and what is already on the map, so it can file, link by number and avoid
repeats. At most two of each kind per turn, confidence 0.6 or above. Only what
you wrote counts; the assistant's reply is context, never a source.

**Interrogation turns** cost nothing extra. Their answers already have a fixed
shape, read without a model:

- *Still dark* and *Accepted dark* become proposed unknowns (at most four).
- The *Probe* becomes a proposed order, aimed at the live unknown or obstacle it
  shares the most words with. Aimed at an unknown, it is reconnaissance.
- A closing turn's *Orders* become proposed orders.

**Open-loop extraction** also sees your active objectives, so a commitment that
plainly serves one is linked to it as it is proposed.

Turn campaign extraction off with `CAMPAIGN_EXTRACTION_ENABLED=false`.

## The theatre map

The campaign page opens with the map: one lane per front, one row per
objective. Your side is on the left, the objective is the diamond on the
right, and the opposition and fog sit on the ground ahead of the **front
line**. Items filed under no active front share a lane at the end. A withdrawn
objective keeps its row, struck through, and counts for nothing.

The line is arithmetic, not judgment. Everything on a row has a weight, and
the line stands at the share of that weight already held:

| On the row | Weight | Held when |
|---|---|---|
| the objective | 2 | taken |
| an obstacle | its strength, 1 to 3 (an unestimated one counts as dug in) | cleared |
| an unknown | 1 | lifted |
| an order aimed at any of them | 1 | done |

A front's line is the same sum across its rows. Ground held is what moved in
the last thirty days, the same window as the rest of the map, so a front
nobody has touched for a month shows its line back at the start: let a front
go quiet and it shows. Proposals and proposed orders are not on the map and
do not count.

The map is drawn on the server as one SVG, with no model in the loop:

- `GET /api/campaign/map.svg` is today's map; `?day=YYYY-MM-DD` is a past one.
- `GET /api/campaign/theatre` is the same map as JSON.
- `GET /api/campaign/timelapse` lists the days on record with each front's
  line and the day's totals.

A dashed line shows where each front stood a week ago (or on the earliest day
on record, when the campaign is younger than that).

**Snapshots.** A ticker refreshes today's snapshot every ten minutes, and once
at startup, writing only when the map changed; the last write of the day is
the day's map. A ticker rather than a midnight job, because a laptop asleep at
midnight would skip the day. Days follow `CHECKIN_TZ`. The timelapse on the
campaign page plays the record oldest first and ends on the live map.

## The staff

The generals read the map, not just the question. Everything they read is
one deterministic reading, the **sitrep**, taken from the live map and
yesterday's snapshot with no model in the loop:

- every front with its line, and where the line stood on the last snapshot
  before today;
- every objective with what stands in front of it and the orders aimed at
  each, by name, with what is due today or overdue; fog marked as scouted
  when a recon order is out against it;
- what moved in the last twenty-four hours: objectives taken, opposition
  cleared, fog lifted, objectives withdrawn, orders carried out, and the
  front lines that moved;
- the **readings**: what the arithmetic says on its own.

| Reading | Means |
|---|---|
| overdue | an active objective past its date |
| overextended | four or more orders open on a front and its line has not moved since the last snapshot |
| quiet | a front with live positions, no open orders, and a line that has not moved |
| fortress | a strength-3 obstacle with nothing aimed at it: go around, or commit |
| unwanted | opposition typed *not really wanted*: a case for withdrawing the objective behind it |
| unscouted | fog with no reconnaissance sent against it |
| estimated | how many enemy strengths nobody has confirmed |
| closest | the live objective with the most ground held |

`GET /api/campaign/sitrep` serves it, and the campaign page shows the
readings in a strip under the map on every load.

**Into every answer.** The sitrep is rendered as a short brief and placed in
the context of every answer, between *about me* and the retrieved memories,
through the memory module's `SituationReader` port. The generals therefore
argue with the map in front of them: an answer about this week can say that
◆ *Ship v2* is overdue, that ▲ *No tests* is a fortress nobody has aimed at,
that the Health front has gone quiet. It costs one database read and no
model call, and an empty map adds nothing, so the room answers as it did
before there was a map. `CAMPAIGN_STAFF_ENABLED=false` turns it off. Plain
check-ins carry their own reading instead (below), and the brief is capped
at four thousand characters so a large map is summarised, not dumped.

**The appraisal.** *Ask the staff* on the campaign page runs the `staff`
mode: a question that names the objectives in play, answered with the map in
context, in a fixed shape — the situation, one point of main effort, where to
yield ground, what is overextended, which fog to lift first, and two or
three orders with a direction, a time and a fallback. The mode seats Manstein
(concentration), Kutuzov (preservation) and Vasilevsky (several fronts at
once), so there is a real fork in it: one lens that concentrates, one that
yields. Standard is two generals in one call; Deep is a council of three,
each reading the map alone before the exchange is written. The result is
stored with its date, generals and depth; the page shows the latest and keeps
the earlier ones. *Discuss* opens it as a conversation, lazily and once, with
the generals who wrote it seated, so arguing back is arguing with them. One
front can be appraised alone from its header. The appraisal runs in the
background on the server, bounded by its tier's deadline and a ten-minute
backstop: the request returns at once and the page polls until the staff
reports back, so a council's eight minutes are never a single request held
open with nothing on the wire, and an appraisal you asked for and walked
away from is still there when you come back. A second request while one is
being written is refused rather than spending a second set of calls, and a
failed one is reported on the page until the next starts.

**Sitreps.** With a map, check-ins are drawn from it. The morning one is
*Morning orders*: today's point of main effort and the order that serves it.
Midday is the *Contact report*: which orders are in contact (due today or
overdue) and whether you are on them. Evening is *Gains and losses*: what
moved on the map today and what did not. Each is still one quick, plain
model call; the sitrep travels in the question. With no map, a check-in is
exactly what it was.

**Red team.** The `red_team` mode hands one general command of the
opposition: the positions marked ▲ are its units, the fog ? its cover, and
each kind of stuck holds the way it holds — *waiting* by your not chasing
the input, *fear* by your not looking, *too big* by never being cut down,
*not really wanted* because you do not want to win. It writes who commands,
how each position holds, three attacks with the early sign you will see when
each is working, the fog it exploits, what it concedes, and what it would
take to beat it. Seat a general to choose who commands; otherwise Manstein,
the operator who wins from a weak position, takes it. Open it from an
objective or a front on the campaign page, from the briefing bar, or from
the mode menu.

A red team turn also costs nothing extra on the map: the attacks are read
out of the answer as proposed opposition, typed by the kind of stuck the red
team gave them, estimated as dug in, and aimed at the objective they share
the most words with; the questions under *Your fog, my cover* become proposed
unknowns. Both carry the `red_team` source, so Intel says they are the
enemy's estimate.

## Logistics

Supply is what feeds a front, and v2.4 reads three kinds of it. All three
are reported by you; none is inferred from what you write, because a supply
line guessed from your tone is one you would stop trusting.

| Supply | Where it comes from |
|---|---|
| **Time** | **sorties**: the focus blocks you log against a front, each optionally on one order. *+ Sortie* logs one default block today; *…* sets its length, its order and its day, up to four weeks back |
| **Energy** | one tap a day, 1 *spent* to 5 *sharp*, under the evening check-in or on the campaign page. A tap after midnight on last night's check-in lands on last night |
| **Money** | the funds you set against a front, against what its live objectives cost (a cost you set on an objective) and what the stays deployed on it cost |

**The arithmetic.** Hours a week are the sortie hours on a front over the
last fourteen days, scaled to a week, so a day off moves the rate a little
rather than by half (a record younger than two weeks is measured over what
there is, at least a week). An order costs one default sortie
(`SORTIE_MINUTES`, 25 by default), marked `est`, until the front has carried
out three orders in the last four weeks; from then on it costs the front's
own rate, sortie hours over orders done. Orders open on a front are the
ones aimed at what is filed under it, the same rule the theatre lays lanes
out by.

**Warnings before collapse.** The readings gain five kinds, all arithmetic:

| Reading | Means |
|---|---|
| outrun | the orders due within a week need more than the front's weekly hours plus one spare sortie, or its backlog would take longer than an order stays fresh (`COMMITMENT_STALE_AFTER`, two weeks) plus one sortie |
| cut | a front with orders due within a week, no sorties in the window, and sorties in the weeks before it: a supply line that stopped |
| overdrawn | a front's costs exceed its funds, with the share of them that is an estimate |
| deployment | a stay deployed on a front lands within two weeks with critical pitfalls unhandled, items still needed, or half its cost estimated |
| energy | most of the last three evenings reported were low or spent |

The one spare sortie is deliberate: a single order more than a thin week
supplies is a stretch, not a front about to collapse. Time readings need a
record: a front you never log a sortie on is never called out of supply,
and until a sortie is logged anywhere, nothing about hours is said at all.
Where sorties do measure a front, the supply decides overextension: an
*outrun* or *cut* reading takes the place of the count-based *overextended*,
and a front its sorties keep up with is not called overextended. A front
with no sorties to judge by is still read by count.

**Money is never converted.** A front's costs are summed in the currency of
its funds, or, with none set, the currency carrying most of them. Costs in
any other currency are listed beside the sum, never converted at a guessed
rate. A cost you put on an objective is yours and confirmed; a deployment's
cost keeps the relocation planner's estimated share, so the map and the
planner never disagree about how much of a stay is a guess.

**Deployments.** *Deployed on* in the relocation planner puts a stay on a
front as a forward base. Its setup budget joins the front's costs; its
arrival date, unhandled critical pitfalls and items still needed are read
before it lands; the campaign page links back to the plan. A stay that is
done, abandoned or already departed is no longer a deployment. The relocation
module provides the deployments through the campaign's own port, so the
campaign never imports the planner.

**Into the brief.** The supply lines are written after the readings, so a
long map is cut there first, and only once something is reported: with no
sorties, no energy and no money, the brief and the readings are exactly what
they were. The morning sitrep is told to pick an order that fits what is
left when a front is outrunning its supply or energy has run low; the
evening one not to ask about energy, since the tap does; the staff
appraisal weighs orders against supply and is told not to invent hours that
were not logged. `GET /api/campaign/logistics` serves the supply lines with
the last two weeks of sorties; the sitrep carries them too.

## Data

`migrations/008_campaign.sql`: `fronts`, one `campaign_items` table for all
three types (they share a lifecycle), and `target_id` and `kind` on
`commitments`. Deleting an item clears the aim of orders pointed at it; the
orders themselves stay.

`migrations/010_campaign_snapshots.sql`: `campaign_snapshots`, one row per
day holding the laid-out map as JSON, so a past day is drawn by the same
renderer as today's.

`migrations/011_staff.sql`: `campaign_appraisals`, what the staff said and
when, with the conversation it was opened into; and `red_team` as a source
for the opposition and fog the red team proposes.

`migrations/012_logistics.sql`: `sorties` (one per focus block, on a
front, optionally on an order; the sortie outlives its order) and
`energy_reports` (one per day, a second tap replaces the first); `funds` and
`funds_currency` on `fronts`; `cost` and `cost_currency` on `campaign_items`,
objectives only; and `front_id` on `relocation_plans`, cleared if the front
goes. A currency is required with every amount.

Not yet: the campaign record (v2.5).
