package params

import "flag"

// CheckRequest is the resolved form of `forecast check`'s flags.
type CheckRequest struct {
	Exclusions string
}

// Check holds the flags of `forecast check`. It has no deprecated -db alias.
type Check struct {
	Input      Input
	Exclusions *string
	Config     *string
}

// NewCheck registers the `check` flags on fs.
func NewCheck(fs *flag.FlagSet) *Check {
	c := &Check{}
	c.Input = AddInput(fs, false)
	c.Exclusions = fs.String("exclusions", "", "path to an exclusions JSON file to validate against the input's assignees")
	c.Config = AddConfig(fs)
	return c
}

// Resolve returns the input path and the exclusions path to validate.
func (c *Check) Resolve() (Resolved[CheckRequest], error) {
	var r Resolved[CheckRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}
	r.Request = CheckRequest{Exclusions: *c.Exclusions}
	return r, nil
}
