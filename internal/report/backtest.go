package report

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// BacktestText writes the `sim backtest` text report.
func BacktestText(w io.Writer, res *app.BacktestResult, sampleStart, sampleEnd, today time.Time) {
	rows := res.Rows
	startDate := res.ReplayStart
	fmt.Fprintf(w, "Backtest: %s (%d issues, %s)\n", res.Scope, res.IssueCount, res.Run.ModeLabel)
	fmt.Fprintf(w, "Start date: %s  Sample window: %s – %s\n\n",
		startDate.Format("2006-01-02"), sampleStart.Format("2006-01-02"), sampleEnd.Format("2006-01-02"))

	// Format the header and every row through tabwriter in one pass so column
	// widths stay consistent; the projected/today divider is spliced into the
	// already-aligned output afterward, since a line with no tab-separated
	// cells would otherwise make tabwriter start a new column block.
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATE\tCOMPLETED\tREMAINING\tPROB")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\n",
			r.Date.Format("2006-01-02"), r.Completed, r.Remaining, r.Prob)
	}
	tw.Flush()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	dividerLine := fmt.Sprintf("---- today: %s — rows below are projected (assuming no further completions) ----",
		today.Format("2006-01-02"))
	dividerIdx := -1
	for i, r := range rows {
		if r.Projected {
			dividerIdx = i + 1 // +1 to account for the header line
			break
		}
	}
	if dividerIdx >= 0 {
		lines = append(lines[:dividerIdx], append([]string{dividerLine}, lines[dividerIdx:]...)...)
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}

// BacktestCSV writes the `sim backtest` CSV report.
func BacktestCSV(w io.Writer, res *app.BacktestResult) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"date", "completed", "remaining", "probability", "projected"}); err != nil {
		return err
	}
	for _, r := range res.Rows {
		if err := cw.Write([]string{
			r.Date.Format("2006-01-02"),
			strconv.Itoa(r.Completed),
			strconv.Itoa(r.Remaining),
			strconv.FormatFloat(r.Prob, 'f', 2, 64),
			strconv.FormatBool(r.Projected),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
