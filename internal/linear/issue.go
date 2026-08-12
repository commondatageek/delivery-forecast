package linear

import "github.com/commondatageek/delivery-forecast/issues"

// Issue is an alias for issues.Issue, retained so existing callers compile
// unchanged. New code should use issues.Issue directly. Removed in Phase 5.
type Issue = issues.Issue
