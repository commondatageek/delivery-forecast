# On-Call Modeling

## The Problem

We have 5 engineers, 1 always on-call at a time. For a simulation of 3 engineers on one track,
each engineer has a 1/5 (0.2) probability of being on-call on any given day.

On-call engineers do less (or zero) project work, depending on the week. Ignoring this
**overestimates throughput** in the simulation.

Currently, `exclusions.json` only covers major holidays — on-call days are not excluded,
so the sample pool implicitly includes on-call days already. However, normal days and
on-call days are mixed together, which dilutes both distributions.

---

## Option 1: Binary on-call (zero output)

Add an `-oncall-fraction` flag (e.g. `0.2`). For each engineer on each simulated day,
roll a die — if on-call, contribute 0 items.

```go
for e := 0; e < numEngineers; e++ {
    if rng.Float64() < oncallFraction {
        continue // on-call, no project work
    }
    total += pool.Draw(rng)
}
```

**Pros:** Simple, one new flag, no extra data needed.
**Cons:** Assumes on-call = zero output, which isn't always true.
**Tuning:** Use a fraction lower than 0.2 (e.g. 0.1) to approximate partial productivity.

---

## Option 2: Two pools — normal and on-call

Build two separate sample pools from historical data:
- **Normal pool**: days when the engineer was not on-call
- **On-call pool**: days when the engineer was on-call (lower throughput, but not zero)

```go
if rng.Float64() < oncallFraction {
    total += oncallPool.Draw(rng)
} else {
    total += pool.Draw(rng)
}
```

**Pros:** Captures the real on-call distribution (some weeks are fine, some are rough).
**Cons:** Requires tagging which days were on-call in the data (e.g. a new `oncall.json` file,
similar in structure to `exclusions.json`). On-call history needs to be reconstructed
(e.g. from PagerDuty or a calendar).

---

## Recommendation

- If on-call history is easy to reconstruct → **Option 2** is more accurate and still clean.
- If tagging on-call days is painful → **Option 1** with a tuned fraction is good enough.

For Option 1, `-oncall-fraction 0.2` is theoretically correct for 1-of-5 engineers being
on-call. Adjust downward if on-call engineers are still partially productive.
