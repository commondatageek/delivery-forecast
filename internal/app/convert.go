package app

import (
	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// toAgingIssues converts issues.Issue records to aging.Issue.
func toAgingIssues(items []issues.Issue) []aging.Issue {
	out := make([]aging.Issue, len(items))
	for i, it := range items {
		out[i] = aging.Issue{
			Identifier:  it.Identifier,
			Title:       it.Title,
			Assignee:    it.Assignee,
			ProjectName: it.ProjectName,
			StateType:   it.StateType,
			StateName:   it.StateName,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
		}
	}
	return out
}

// toCFDIssues converts issues.Issue records to cfd.Issue.
func toCFDIssues(items []issues.Issue) []cfd.Issue {
	out := make([]cfd.Issue, len(items))
	for i, it := range items {
		out[i] = cfd.Issue{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
			CanceledAt:  it.CanceledAt,
			StateType:   it.StateType,
		}
	}
	return out
}

// toHistoryIssues converts issues.Issue records to history.Issue.
func toHistoryIssues(items []issues.Issue) []history.Issue {
	out := make([]history.Issue, len(items))
	for i, it := range items {
		out[i] = history.Issue{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
			CanceledAt:  it.CanceledAt,
		}
	}
	return out
}

// toCountsIssues converts issues.Issue records to counts.Issue.
//
// counts.Aggregate decides "terminal" from StateType alone, so a file source
// that carries completed_at/canceled_at but no state_type would otherwise
// count every finished issue as outstanding. Terminal issues are therefore
// given a terminal StateType here, via issues.Issue.IsCompleted/IsCanceled —
// the same timestamp-first rule every other command uses.
func toCountsIssues(items []issues.Issue) []counts.Issue {
	out := make([]counts.Issue, len(items))
	for i, it := range items {
		stateType := it.StateType
		switch {
		case stateType == "completed" || stateType == "canceled" || stateType == "duplicate":
			// Already terminal; keep it as-is (preserves duplicate).
		case it.IsCompleted():
			stateType = "completed"
		case it.IsCanceled():
			stateType = "canceled"
		}
		out[i] = counts.Issue{
			TeamKey:              it.TeamKey,
			TeamName:             it.TeamName,
			ProjectName:          it.ProjectName,
			ProjectMilestoneName: it.ProjectMilestoneName,
			StateType:            stateType,
			UpdatedAt:            it.UpdatedAt,
		}
	}
	return out
}

// issuesToCompletions converts issues.Issue records to simulate.Completion.
// No filtering is performed; call simulate.FilterInvalid on the result.
func issuesToCompletions(items []issues.Issue) []simulate.Completion {
	records := make([]simulate.Completion, len(items))
	for i, it := range items {
		records[i] = simulate.Completion{Engineer: it.Assignee, CompletedAt: it.CompletedAt}
	}
	return records
}

// issuesToBacktestItems converts issues.Issue records to simulate.BacktestItem.
func issuesToBacktestItems(items []issues.Issue) []simulate.BacktestItem {
	out := make([]simulate.BacktestItem, len(items))
	for i, it := range items {
		out[i] = simulate.BacktestItem{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
		}
	}
	return out
}

// Deprecated shims for cmd/forecast during the migration; removed in Phase 4.

// ToAgingIssues is a temporary export of toAgingIssues.
func ToAgingIssues(items []issues.Issue) []aging.Issue { return toAgingIssues(items) }

// ToCFDIssues is a temporary export of toCFDIssues.
func ToCFDIssues(items []issues.Issue) []cfd.Issue { return toCFDIssues(items) }

// ToHistoryIssues is a temporary export of toHistoryIssues.
func ToHistoryIssues(items []issues.Issue) []history.Issue { return toHistoryIssues(items) }

// ToCountsIssues is a temporary export of toCountsIssues.
func ToCountsIssues(items []issues.Issue) []counts.Issue { return toCountsIssues(items) }

// IssuesToCompletions is a temporary export of issuesToCompletions.
func IssuesToCompletions(items []issues.Issue) []simulate.Completion {
	return issuesToCompletions(items)
}

// IssuesToBacktestItems is a temporary export of issuesToBacktestItems.
func IssuesToBacktestItems(items []issues.Issue) []simulate.BacktestItem {
	return issuesToBacktestItems(items)
}
