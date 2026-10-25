package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// describeWindow renders a forecast horizon for report headers, e.g.
// "2026-10-08 to 2026-11-06 (30 days)".
func describeWindow(start, end time.Time, days int) string {
	return fmt.Sprintf("%s to %s (%d days)", start.Format("2006-01-02"), end.Format("2006-01-02"), days)
}

// SimItems writes the `sim items` report.
func SimItems(w io.Writer, res *app.ItemsResult) {
	fmt.Fprintf(w, "%s, %s -> how many items?\n\n", res.Run.ModeLabel, describeWindow(res.TargetStart, res.TargetEnd, res.EffectiveDays))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Confidence\tItems")
	for _, row := range res.Rows {
		fmt.Fprintf(tw, "%d%%\tat least %d\n", row.Confidence, row.Items)
	}
	tw.Flush()
}

// SimDays writes the `sim days` report: the grouped trajectory when more than
// one item count was requested, otherwise the single-value table.
func SimDays(w io.Writer, res *app.DaysResult, items []int) {
	if len(items) > 1 {
		trajectory(w, res)
		return
	}

	fmt.Fprintf(w, "%s, %d items -> how many days?\n\n", res.Run.ModeLabel, items[0])

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Confidence\tDays\tDate")
	for _, row := range res.Single {
		fmt.Fprintf(tw, "%d%%\t%d\t%s\n", row.Confidence, row.Days, row.Date.Format("2006-01-02 Mon"))
	}
	tw.Flush()
}

// trajectory prints the grouped trajectory report for `sim days
// -items g1,g2,...`: one row per group plus a Total row, with per-confidence
// Days/Date columns. All thresholds are simulated with the same seed (see
// the trajectory table in package simulate) so the report's invariants hold.
func trajectory(w io.Writer, res *app.DaysResult) {
	fmt.Fprintf(w, "%s, starting %s -> grouped trajectory\n\n", res.Run.ModeLabel, res.TargetStart.Format("2006-01-02"))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	confRow := []string{"", ""}
	header := []string{"Group", "Items"}
	for _, c := range res.Confidences {
		confRow = append(confRow, fmt.Sprintf("%d%%", c), "")
		header = append(header, "Days", "Date")
	}
	fmt.Fprintln(tw, strings.Join(confRow, "\t"))
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	for g, group := range res.Groups {
		row := []string{fmt.Sprintf("Group %d", g+1), fmt.Sprintf("%d", group.Items)}
		for _, cell := range group.Cells {
			row = append(row, fmt.Sprintf("%d", cell.MarginalDays), cell.Date.Format("2006-01-02 (Mon)"))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}

	totalRow := []string{"Total", fmt.Sprintf("%d", res.Total.Items)}
	for _, cell := range res.Total.Cells {
		totalRow = append(totalRow, fmt.Sprintf("%d", cell.MarginalDays), cell.Date.Format("2006-01-02 (Mon)"))
	}
	fmt.Fprintln(tw, strings.Join(totalRow, "\t"))

	tw.Flush()
}

// SimProbability writes the `sim probability` report.
func SimProbability(w io.Writer, res *app.ProbabilityResult) {
	windowDescription := describeWindow(res.TargetStart, res.TargetEnd, res.EffectiveDays)

	if res.Items >= 0 {
		fmt.Fprintf(w, "%s, %s, %d items -> probability of completion?\n", res.Run.ModeLabel, windowDescription, res.Items)
		fmt.Fprintf(w, "  %.1f%%  (i.e. you can commit to %d items here at ~%.0f%% confidence)\n", res.Probability, res.Items, res.Probability)
	} else {
		fmt.Fprintf(w, "%s, %s -> probability of completing N items\n", res.Run.ModeLabel, windowDescription)
		fmt.Fprintf(w, "  (each row's %% is also that many items' confidence level; see `sim items -confidence`)\n")
		for _, row := range res.Table {
			fmt.Fprintf(w, "  %d items: %.1f%%\n", row.Items, row.Probability)
		}
	}
}
