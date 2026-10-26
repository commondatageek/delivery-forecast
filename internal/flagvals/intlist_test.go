package flagvals

import (
	"reflect"
	"testing"
)

func TestIntList_Set(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want IntList
	}{
		{"single value", "50", IntList{50}},
		{"multiple values", "50,75,90", IntList{50, 75, 90}},
		{"trims surrounding whitespace", " 50 , 75 ", IntList{50, 75}},
		{"negative values", "-1,2,-3", IntList{-1, 2, -3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p IntList
			if err := p.Set(tt.val); err != nil {
				t.Fatalf("Set(%q) returned error: %v", tt.val, err)
			}
			if !reflect.DeepEqual(p, tt.want) {
				t.Fatalf("Set(%q) = %#v, want %#v", tt.val, p, tt.want)
			}
		})
	}
}

func TestIntList_Set_Errors(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{"empty string", ""},
		{"only commas", ",,,"},
		{"only whitespace", "   "},
		{"non-numeric part", "50,abc,90"},
		{"float value", "50.5"},
		{"trailing comma", "50,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p IntList
			if err := p.Set(tt.val); err == nil {
				t.Fatalf("Set(%q) returned no error, want error", tt.val)
			}
		})
	}
}

func TestIntList_Set_ResetsExistingValue(t *testing.T) {
	p := IntList{1, 2, 3}
	if err := p.Set("9"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	want := IntList{9}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("Set did not reset prior contents: got %#v, want %#v", p, want)
	}
}

func TestIntList_Set_LeavesPriorValueOnError(t *testing.T) {
	p := IntList{1, 2, 3}
	if err := p.Set("1,abc"); err == nil {
		t.Fatalf("Set returned no error, want error")
	}
	want := IntList{1}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("Set on error = %#v, want %#v", p, want)
	}
}

func TestIntList_String(t *testing.T) {
	tests := []struct {
		name string
		p    IntList
		want string
	}{
		{"nil list", nil, ""},
		{"empty list", IntList{}, ""},
		{"single value", IntList{50}, "50"},
		{"multiple values", IntList{50, 75, 90}, "50,75,90"},
		{"negative values", IntList{-1, 2, -3}, "-1,2,-3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntList_SetThenStringRoundTrip(t *testing.T) {
	var p IntList
	if err := p.Set(" 50, 75 ,90"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, want := p.String(), "50,75,90"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
