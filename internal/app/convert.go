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
			Status:      string(it.Status),
			StatusName:  it.StatusName,
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

// toCountsIssues converts issues.Issue records to counts.Issue. Terminal is
// issues.Issue.IsTerminal — the same timestamp-first rule every other
// command uses, so a file with completed_at/canceled_at but no status still
// counts finished work as finished.
func toCountsIssues(items []issues.Issue) []counts.Issue {
	out := make([]counts.Issue, len(items))
	for i, it := range items {
		out[i] = counts.Issue{
			TeamKey:              it.TeamKey,
			TeamName:             it.TeamName,
			ProjectName:          it.ProjectName,
			ProjectMilestoneName: it.MilestoneName,
			Terminal:             it.IsTerminal(),
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
