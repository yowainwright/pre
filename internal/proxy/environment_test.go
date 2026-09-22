package proxy

import "testing"

func TestEnvFlagTruths(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PRE_TEST_FLAG", value)
			if !envFlag("PRE_TEST_FLAG") {
				t.Fatalf("expected %q to enable flag", value)
			}
		})
	}
}

func TestEnvFlagFalseValues(t *testing.T) {
	for _, value := range []string{"", "0", "false", "no", "off", "anything"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PRE_TEST_FLAG", value)
			if envFlag("PRE_TEST_FLAG") {
				t.Fatalf("expected %q to disable flag", value)
			}
		})
	}
}

func TestPackageLimitExceeded(t *testing.T) {
	t.Setenv(envMaxPackages, "2")
	limit, exceeded := packageLimitExceeded(3)
	unexpectedLimit := limit != 2 || !exceeded
	if unexpectedLimit {
		t.Fatalf("expected limit 2 to be exceeded, got limit=%d exceeded=%v", limit, exceeded)
	}
	if _, exceeded := packageLimitExceeded(2); exceeded {
		t.Fatal("expected package count at limit to be allowed")
	}
}

func TestPackageLimitInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "many"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(envMaxPackages, value)
			limit, exceeded := packageLimitExceeded(100)
			unexpectedLimit := limit != 0 || exceeded
			if unexpectedLimit {
				t.Fatalf("expected invalid limit %q to be ignored, got limit=%d exceeded=%v", value, limit, exceeded)
			}
		})
	}
}
