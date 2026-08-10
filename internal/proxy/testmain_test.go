package proxy

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.") {
			os.Exit(m.Run())
		}
	}
	os.Exit(0)
}
