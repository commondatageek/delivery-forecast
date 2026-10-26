package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const maxStoredRuns = 50

// runStore keeps the last 50 successful runs, evicting the oldest.
type runStore struct {
	mu    sync.Mutex
	order []string
	runs  map[string]*storedRun
}

type storedRun struct {
	ID, Command string
	Created     time.Time
	Out         *runOutput
}

func newRunStore() *runStore { return &runStore{runs: map[string]*storedRun{}} }

func newRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "r" + hex.EncodeToString(b[:])
}

// add stores a finished run and returns its id.
func (st *runStore) add(command string, created time.Time, out *runOutput) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	id := newRunID()
	st.runs[id] = &storedRun{ID: id, Command: command, Created: created, Out: out}
	st.order = append(st.order, id)
	for len(st.order) > maxStoredRuns {
		delete(st.runs, st.order[0])
		st.order = st.order[1:]
	}
	return id
}

func (st *runStore) get(id string) (*storedRun, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r, ok := st.runs[id]
	return r, ok
}
