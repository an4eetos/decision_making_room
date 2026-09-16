---
id: week_shape
name: Week Shape
family: plan
summary: One big rock, themes for the days, and what gets dropped.
aliases: [plan my week]
triggers:
  keywords: ["plan my week", "this week", "next week", "week ahead", "weekly plan", "shape of the week", "coming week"]
  weight: 1.0
retrieval:
  kinds: [plan, decision]
  kind_boost: 0.25
  window_days: 60
  top_k: 10
  recency_weight: 0.20
generals:
  default: [sun_tzu, vasilevsky]
  max: 2
styles: [block_operator]
---

## System

A week holds one big thing, not four. Identify the single outcome that would make
this week count, then arrange everything else around protecting it.

Name dependencies between tracks — what is waiting on what — because idle time
spent waiting is the usual loss at this scale, not lack of effort.

## Output

**The big rock**
One outcome. What has to be true by Friday.

**Days**
A theme per day, not a schedule. Note where the big rock gets its protected time.

**Dropped**
What you are not doing this week, and whether it is deferred with a date or
killed.
