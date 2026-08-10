package manager

import "testing"

func TestParseSpec(t *testing.T) {
	tests := []struct {
		ecosystem string
		spec      string
		wantName  string
		wantVer   string
	}{
		{"unknown", "somepkg", "somepkg", ""},
		{"npm", "react", "react", ""},
		{"npm", "react@18.0.0", "react", "18.0.0"},
		{"npm", "@scope/pkg", "@scope/pkg", ""},
		{"npm", "@scope/pkg@1.0.0", "@scope/pkg", "1.0.0"},
		{"Go", "github.com/foo/bar", "github.com/foo/bar", ""},
		{"Go", "github.com/foo/bar@v1.2.3", "github.com/foo/bar", "v1.2.3"},
		{"crates.io", "serde", "serde", ""},
		{"crates.io", "serde@1.0.217", "serde", "1.0.217"},
		{"PyPI", "requests", "requests", ""},
		{"PyPI", "requests==2.28.0", "requests", "2.28.0"},
		{"PyPI", "requests>=1.0", "requests", ">=1.0"},
		{"PyPI", "requests<=2.0", "requests", "<=2.0"},
		{"PyPI", "requests>1.0", "requests", ">1.0"},
		{"PyPI", "requests<2.0", "requests", "<2.0"},
		{"PyPI", "requests!=1.5", "requests", "!=1.5"},
		{"PyPI", "requests~=1.0", "requests", "~=1.0"},
		{"PyPI", "urllib3<1.26", "urllib3", "<1.26"},
		{"PyPI", "requests[socks]==2.19.0", "requests", "2.19.0"},
		{"PyPI", "requests >=1,<2", "requests", ">=1,<2"},
		{"PyPI", "requests @ https://example.com/requests.whl", "requests", "@ https://example.com/requests.whl"},
		{"PyPI", "requests invalid", "requests", unsupportedPyRequirement},
		{"Homebrew", "nginx", "nginx", ""},
		{"Homebrew", "openssl@3", "openssl@3", ""},
		{"Homebrew", "git@@2.43.0", "git", "2.43.0"},
	}

	for _, tc := range tests {
		name, ver := ParseSpec(tc.ecosystem, tc.spec)
		if name != tc.wantName {
			t.Errorf("ParseSpec(%q, %q): name = %q, want %q", tc.ecosystem, tc.spec, name, tc.wantName)
		}
		if ver != tc.wantVer {
			t.Errorf("ParseSpec(%q, %q): version = %q, want %q", tc.ecosystem, tc.spec, ver, tc.wantVer)
		}
	}
}
