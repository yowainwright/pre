package manager

import "testing"

func TestSelectCargoVersion(t *testing.T) {
	versions := []crateVersionInfo{
		{Num: "2.0.0"},
		{Num: "1.8.0"},
		{Num: "1.7.0", Yanked: true},
		{Num: "1.3.0"},
		{Num: "1.2.9"},
		{Num: "1.2.3"},
		{Num: "1.1.9"},
		{Num: "0.2.5"},
		{Num: "0.2.3"},
		{Num: "1.9.0-alpha.1"},
	}
	tests := map[string]string{
		"^1.2.3":     "1.8.0",
		"~1.2.3":     "1.2.9",
		"1.*":        "1.8.0",
		">=1.2, <2":  "1.8.0",
		"=1.2.3":     "1.2.3",
		"=1.2":       "1.2.9",
		">1.2, <1.8": "1.3.0",
		"<=1.2":      "1.2.9",
		"<1.2":       "1.1.9",
		">1":         "2.0.0",
		"<=1":        "1.8.0",
		"^0.2.3":     "0.2.5",
	}
	for requirement, want := range tests {
		got, ok := selectCargoVersion(versions, requirement)
		if !ok || got != want {
			t.Errorf("selectCargoVersion(%q) = %q, %v; want %q", requirement, got, ok, want)
		}
	}
}

func TestSelectCargoPrereleaseWithXInIdentifier(t *testing.T) {
	versions := []crateVersionInfo{{Num: "1.9.0-rcx.1"}, {Num: "1.9.0-rcx.2"}, {Num: "1.10.0-beta.1"}}
	version, ok := selectCargoVersion(versions, "^1.9.0-rcx.1")
	if !ok || version != "1.9.0-rcx.2" {
		t.Errorf("expected rcx.2, got %q, %v", version, ok)
	}
}

func TestCargoPartialComparators(t *testing.T) {
	tests := []struct {
		requirement string
		version     cargoSemver
		want        bool
	}{
		{requirement: "=1.2", version: cargoSemver{major: 1, minor: 2, patch: 9}, want: true},
		{requirement: ">1.2", version: cargoSemver{major: 1, minor: 2, patch: 9}, want: false},
		{requirement: ">1.2", version: cargoSemver{major: 1, minor: 3}, want: true},
		{requirement: "<=1.2", version: cargoSemver{major: 1, minor: 2, patch: 9}, want: true},
		{requirement: "<=1.2", version: cargoSemver{major: 1, minor: 3}, want: false},
	}
	for _, test := range tests {
		if got := cargoRequirementMatches(test.requirement, test.version); got != test.want {
			t.Errorf("cargoRequirementMatches(%q, %#v) = %v; want %v", test.requirement, test.version, got, test.want)
		}
	}
}
