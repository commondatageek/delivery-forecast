package main

import (
	"reflect"
	"testing"
)

func TestStringList_Set(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want stringList
	}{
		{"single value", "alice", stringList{"alice"}},
		{"multiple values", "alice,bob,carol", stringList{"alice", "bob", "carol"}},
		{"trims surrounding whitespace", " alice , bob ", stringList{"alice", "bob"}},
		{"drops empty parts", "alice,,bob,", stringList{"alice", "bob"}},
		{"empty string yields nil", "", nil},
		{"only commas yields nil", ",,,", nil},
		{"only whitespace yields nil", "   ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s stringList
			if err := s.Set(tt.val); err != nil {
				t.Fatalf("Set(%q) returned error: %v", tt.val, err)
			}
			if !reflect.DeepEqual(s, tt.want) {
				t.Fatalf("Set(%q) = %#v, want %#v", tt.val, s, tt.want)
			}
		})
	}
}

func TestStringList_Set_ResetsExistingValue(t *testing.T) {
	s := stringList{"old", "values"}
	if err := s.Set("new"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	want := stringList{"new"}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("Set did not reset prior contents: got %#v, want %#v", s, want)
	}
}

func TestStringList_String(t *testing.T) {
	tests := []struct {
		name string
		s    stringList
		want string
	}{
		{"nil list", nil, ""},
		{"empty list", stringList{}, ""},
		{"single value", stringList{"alice"}, "alice"},
		{"multiple values", stringList{"alice", "bob", "carol"}, "alice,bob,carol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStringList_SetThenStringRoundTrip(t *testing.T) {
	var s stringList
	if err := s.Set("alice, bob ,carol"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, want := s.String(), "alice,bob,carol"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
