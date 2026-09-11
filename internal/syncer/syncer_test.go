package syncer

import "testing"

func TestSelectVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		constraint string
		want       string
	}{
		{name: "no constraint", constraint: "", want: ""},
		{name: "exact version", constraint: "[1.2.3]", want: "1.2.3"},
		{name: "minimum range", constraint: "[1.2.3,)", want: ""},
		{name: "bounded range", constraint: "[1.2,2.0)", want: ""},
		{name: "floating minimum", constraint: "1.2.3", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := selectVersion(tt.constraint); got != tt.want {
				t.Fatalf("selectVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}
