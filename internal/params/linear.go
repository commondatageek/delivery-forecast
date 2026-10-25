package params

import (
	"flag"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/syncer"
)

// LinearSync holds the flags of `forecast linear sync`. Here -db is the
// database to write, not the deprecated alias of -input.
type LinearSync struct {
	DB         *string
	Teams      *linear.TeamKeyList
	AllTeams   *bool
	FullReload *bool
	Config     *string
}

// NewLinearSync registers the `linear sync` flags on fs.
func NewLinearSync(fs *flag.FlagSet) *LinearSync {
	c := &LinearSync{}
	c.DB = fs.String("db", "", "path to SQLite database")
	c.Teams = AddTeams(fs, "comma-separated team keys, e.g. ENG,DESIGN; limits the candidate team set")
	c.AllTeams = fs.Bool("all-teams", false, "expand the candidate team set to every accessible Linear team; mutually exclusive with -teams")
	c.FullReload = fs.Bool("full-reload", false, "ignore each team's stored watermark and do a full reload")
	c.Config = AddConfig(fs)
	return c
}

// Resolve requires -db and returns the sync options; InputPath is the db path.
func (c *LinearSync) Resolve() (Resolved[syncer.Options], error) {
	var r Resolved[syncer.Options]
	if *c.DB == "" {
		return r, &app.FieldError{Fields: []string{"db"}, Msg: "-db is required"}
	}
	r.InputPath = *c.DB
	r.Request = syncer.Options{
		Teams:      *c.Teams,
		AllTeams:   *c.AllTeams,
		FullReload: *c.FullReload,
	}
	return r, nil
}

// LinearTeams holds the flags of `forecast linear teams`: only -config.
type LinearTeams struct {
	Config *string
}

// NewLinearTeams registers the `linear teams` flags on fs.
func NewLinearTeams(fs *flag.FlagSet) *LinearTeams {
	return &LinearTeams{Config: AddConfig(fs)}
}
