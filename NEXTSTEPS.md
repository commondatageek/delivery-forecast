# Next Steps
- CFD
  - How to handle the fact that some tickets were never properly "started"? Or were maybe "started" several days after their actual start date?

## Historical Things
- CFD
- A chart showing how the probability of success of hitting a certain target date has changed over time.
- ~~A chart showing how p85 for Cycle Time has changed over time.~~ Data now available: `forecast history`'s `CycleTimeP50`/`CycleTimeP85` columns give this per day (see `history` package, `HISTORY_PLAN.md`). The chart itself — rendering it — is still TODO.

## Simulation Report

## Performance
- Calculate a gradient on deltas between simulation runs, and auto-stop when we've converged
  - Prove that running it longer doesn't lead to different outcomes

## Representative Population of Throughput
- Carve out multiple time periods
  - Right now just start and end date
  - But we should be able to say "This month and also that other month"
- Have a sliding window of "historical data" and "data from since we started this project", with historical data eventually sliding off of the window.

## CFDs
- Because how else can we know if our predictions are good or not?
- And it drives home the point that this is a system that needs to be tuned, not an outcomes that needs tampering

## Source abstraction
Use issue data from multiple source types:
- ~~one or more text files~~ Done: every `forecast` command accepts `-input <file>`, reading CSV or JSON directly (`issues` package) as an alternative to SQLite — no sync step required. See "Bring your own data" in `README.md` and `DATA_REQUIREMENTS.md`'s CSV/JSON format section.
  - ~~JSON~~ done
  - ~~CSV~~ done
  - other formats? XSLX?
- authenticated connection to project management backend

## Multiple project management backends
- Create a generic data model
- Create a project-management backend integration abstraction
  - So far, just Get issues with filtering
- Concrete implementations handle authentication, etc.
- Create an integration for Linear
- Create an integration for Jira

## Simulation
- An optional step in the simulation that makes a daily draw on a distribution of "added tickets per day" in order to model how scope increases over time.
- A YAML configuration for the simulation
- Have the simulation logic count issues for you.
  - You might be working on Project A, but do you have 3 non-project tickets open right now, too?  Those must be considered in Project A's projection.
  - The Linear web interface isn't totally clear on how many issues are left (are sub-issues shown? is it grouped in some way?)
  - So we should be able to get a count as "19 in Project A + 3 No Project".
