package app

// FieldError is a validation error attributable to one or more request
// fields. Fields holds the CLI flag names (== -config YAML keys) without the
// leading dash, e.g. []string{"days", "target-end-date"}. Error returns Msg
// unchanged, so CLI output is identical to the pre-refactor plain errors.
type FieldError struct {
	Fields []string
	Msg    string
}

func (e *FieldError) Error() string { return e.Msg }
