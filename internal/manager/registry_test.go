package manager

import (
	"testing"
)

func resetExtraManagers() func() {
	orig := extraManagers
	return func() { extraManagers = orig }
}

func TestAllReturnsBuiltins(t *testing.T) {
	defer resetExtraManagers()()
	extraManagers = nil
	managers := All()
	if len(managers) < len(builtins) {
		t.Errorf("expected at least %d managers, got %d", len(builtins), len(managers))
	}
}

func TestGetNpm(t *testing.T) {
	mgr := Get("npm")
	if mgr == nil {
		t.Fatal("expected non-nil manager for 'npm'")
	}
	if mgr.Name != "npm" {
		t.Errorf("expected name 'npm', got %q", mgr.Name)
	}
}

func TestGetUnknown(t *testing.T) {
	if Get("unknown-xyz") != nil {
		t.Error("expected nil manager for unknown name")
	}
}

func TestSetUserManagersAppend(t *testing.T) {
	defer resetExtraManagers()()
	SetUserManagers([]Manager{
		{Name: "yarn", Ecosystem: "npm", InstallCmds: []string{"add"}},
	})
	mgr := Get("yarn")
	if mgr == nil {
		t.Fatal("expected yarn manager to be available")
	}
	if mgr.Ecosystem != "npm" {
		t.Errorf("expected npm ecosystem, got %q", mgr.Ecosystem)
	}
}

func TestSetUserManagersOverride(t *testing.T) {
	defer resetExtraManagers()()
	SetUserManagers([]Manager{
		{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "ci"}},
	})
	mgr := Get("npm")
	if mgr == nil {
		t.Fatal("expected npm manager")
	}
	hasCI := false
	for _, cmd := range mgr.InstallCmds {
		if cmd == "ci" {
			hasCI = true
		}
	}
	if !hasCI {
		t.Error("expected overridden npm manager to include 'ci'")
	}
}

func TestMergeManagersEmpty(t *testing.T) {
	result := mergeManagers(builtins, nil)
	if len(result) != len(builtins) {
		t.Errorf("expected %d managers, got %d", len(builtins), len(result))
	}
}

func TestMergeManagersAppend(t *testing.T) {
	extra := []Manager{{Name: "yarn", Ecosystem: "npm", InstallCmds: []string{"add"}}}
	result := mergeManagers(builtins, extra)
	if len(result) != len(builtins)+1 {
		t.Errorf("expected %d managers, got %d", len(builtins)+1, len(result))
	}
}

func TestMergeManagersOverride(t *testing.T) {
	extra := []Manager{{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"ci"}}}
	result := mergeManagers(builtins, extra)
	if len(result) != len(builtins) {
		t.Errorf("expected same count after override, got %d", len(result))
	}
	for _, m := range result {
		if m.Name == "npm" && m.InstallCmds[0] != "ci" {
			t.Error("expected npm to be overridden")
		}
	}
}
