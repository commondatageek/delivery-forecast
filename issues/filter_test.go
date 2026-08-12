package issues

import "testing"

func TestFilter_EmptyIsIdentity(t *testing.T) {
	in := []Issue{{Identifier: "A"}, {Identifier: "B"}}
	out := Filter{}.Apply(in)
	if len(out) != len(in) {
		t.Fatalf("got %d issues, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i].Identifier != in[i].Identifier {
			t.Errorf("index %d: got %q, want %q", i, out[i].Identifier, in[i].Identifier)
		}
	}
}

func TestFilter_TeamsCaseInsensitive(t *testing.T) {
	in := []Issue{
		{Identifier: "A", TeamKey: "ENG"},
		{Identifier: "B", TeamKey: "data"},
		{Identifier: "C", TeamKey: "DESIGN"},
	}
	out := Filter{Teams: []string{"eng", "Data"}}.Apply(in)
	if len(out) != 2 {
		t.Fatalf("got %d issues, want 2", len(out))
	}
	if out[0].Identifier != "A" || out[1].Identifier != "B" {
		t.Errorf("got %+v", out)
	}
}

func TestFilter_ProjectAndMilestone(t *testing.T) {
	in := []Issue{
		{Identifier: "A", ProjectName: "Foo", ProjectMilestoneName: "M1"},
		{Identifier: "B", ProjectName: "Foo", ProjectMilestoneName: "M2"},
		{Identifier: "C", ProjectName: "Bar", ProjectMilestoneName: "M1"},
	}
	out := Filter{Project: "Foo", Milestone: "M1"}.Apply(in)
	if len(out) != 1 || out[0].Identifier != "A" {
		t.Fatalf("got %+v", out)
	}

	out = Filter{Project: "Foo"}.Apply(in)
	if len(out) != 2 || out[0].Identifier != "A" || out[1].Identifier != "B" {
		t.Fatalf("got %+v", out)
	}
}

func TestFilter_PreservesOrder(t *testing.T) {
	in := []Issue{
		{Identifier: "C", TeamKey: "ENG"},
		{Identifier: "A", TeamKey: "ENG"},
		{Identifier: "B", TeamKey: "ENG"},
	}
	out := Filter{Teams: []string{"ENG"}}.Apply(in)
	if len(out) != 3 || out[0].Identifier != "C" || out[1].Identifier != "A" || out[2].Identifier != "B" {
		t.Fatalf("order not preserved: %+v", out)
	}
}
