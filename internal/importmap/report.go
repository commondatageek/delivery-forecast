package importmap

// Report summarizes an Apply: what was read, dropped and imported, how
// statuses were decided, where timestamps came from and how full the other
// fields are. JSON tags let the web UI serve it as-is.
type Report struct {
	Source     string        `json:"source"`     // the mapping's source
	Rows       int           `json:"rows"`       // data rows read
	Columns    int           `json:"columns"`    // header width
	Excluded   []ValueCount  `json:"excluded"`   // Label "Issue Type = Epic", rule then first-seen order
	Imported   int           `json:"imported"`   // issues produced
	Status     []StatusCount `json:"status"`     // backlog, started, completed, canceled (+ "unknown" only if > 0)
	Timestamps []FieldSource `json:"timestamps"` // every *_at target mapped, in v2 column order
	Fields     []FieldFill   `json:"fields"`     // every non-timestamp target mapped, in v2 column order
}

// ValueCount is a labeled count.
type ValueCount struct {
	Label string `json:"label"`
	N     int    `json:"n"`
}

// StatusCount is how many imported rows got one status, and why: one Via
// entry per distinct (rule, value) that produced it. Label is the matched
// value as it first appeared in the data, suffixed " (override)" for a
// status-name override and " (resolution)" for a resolution override;
// override entries come first, then resolution, then category matches,
// each in first-seen order.
type StatusCount struct {
	Status string       `json:"status"`
	N      int          `json:"n"`
	Via    []ValueCount `json:"via"`
}

// FieldSource is how many imported rows have a timestamp set and which
// header each value came from, in the rule's header order.
type FieldSource struct {
	Target string       `json:"target"`
	Set    int          `json:"set"`  // rows where it ended up non-zero
	From   []ValueCount `json:"from"` // Label = header the value came from
}

// FieldFill is how many imported rows have a non-empty value for a target.
type FieldFill struct {
	Target string `json:"target"`
	Filled int    `json:"filled"`
}
