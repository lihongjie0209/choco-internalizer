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

func TestParsePackageSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, spec, wantID, wantVersion string
		wantError                       bool
	}{
		{name: "latest", spec: "python", wantID: "python"},
		{name: "pinned prerelease", spec: "python3@3.15.0-rc2", wantID: "python3", wantVersion: "[3.15.0-rc2]"},
		{name: "pinned stable", spec: "dotnetfx@4.8.0.20220524", wantID: "dotnetfx", wantVersion: "[4.8.0.20220524]"},
		{name: "missing version", spec: "dotnetfx@", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePackageSpec(tt.spec)
			if (err != nil) != tt.wantError {
				t.Fatalf("parsePackageSpec(%q) error = %v, wantError %v", tt.spec, err, tt.wantError)
			}
			if got.ID != tt.wantID || got.Version != tt.wantVersion {
				t.Fatalf("parsePackageSpec(%q) = %#v, want ID %q version %q", tt.spec, got, tt.wantID, tt.wantVersion)
			}
		})
	}
}
