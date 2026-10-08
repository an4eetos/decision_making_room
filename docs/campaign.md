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

## Data

`migrations/008_campaign.sql`: `fronts`, one `campaign_items` table for all
three types (they share a lifecycle), and `target_id` and `kind` on
`commitments`. Deleting an item clears the aim of orders pointed at it; the
orders themselves stay.

Not yet: the theatre map (v2.2), the staff reading the map (v2.3), and supply
per front (v2.4).
