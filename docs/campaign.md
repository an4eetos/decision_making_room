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

## Data

`migrations/008_campaign.sql`: `fronts`, one `campaign_items` table for all
three types (they share a lifecycle), and `target_id` and `kind` on
`commitments`. Deleting an item clears the aim of orders pointed at it; the
orders themselves stay.

`migrations/010_campaign_snapshots.sql`: `campaign_snapshots`, one row per
day holding the laid-out map as JSON, so a past day is drawn by the same
renderer as today's.

Not yet: the staff reading the map (v2.3) and supply per front (v2.4).
