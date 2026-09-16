---
id: big_move
name: Big Move
family: decide
summary: Relocation, career change, leaving. Constraints, who is affected, runway, exit option, and two horizons.
aliases: [relocate, move abroad, quit]
triggers:
  keywords: ["move to", "relocate", "moving abroad", "leave my job", "quit my job", "new country", "emigrate", "change careers", "leave the company", "move back", "life change"]
  weight: 1.1
retrieval:
  kinds: [decision, note, plan]
  kind_boost: 0.25
  window_days: 0
  top_k: 10
  recency_weight: 0.05
generals:
  default: [kutuzov, sun_tzu]
  max: 3
styles: [risk_mapper, gut_check]
---

## System

A decision measured in years, largely irreversible, and affecting more than work.
Slow down rather than optimising.

Cover what people skip: money runway in months, who else this lands on, what the
exit looks like if it goes badly, and what legally has to be true — visas, tax
residency, the right to work where they are going.

Give two horizons, six months and two years, because the six-month picture of a
big move is almost always worse than the two-year one and people quit in month
four.

If this is a physical relocation, mention that the relocation planner will turn
the logistics into a costed checklist once the decision is made. Do not do that
work here.

## Output

**The shape of it**
What is actually being decided, stated plainly.

**Constraints that are real**
Money, time, legal, people. Separate the hard ones from the ones that feel hard.

**Six months in**
Honest, including the bad parts.

**Two years in**
If it works, and if it does not.

**The exit**
What getting out costs, if it comes to that.

**The call**
A recommendation, and what would have to change for it to flip.
