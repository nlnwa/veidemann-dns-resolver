package single

import (
	"testing"

	"github.com/coredns/caddy"
)

func TestSetup(t *testing.T) {
	c := caddy.NewTestController("dns", `single`)
	if err := setup(c); err != nil {
		t.Errorf("Test 1, expected no errors, but got: %q", err)
	}

	c = caddy.NewTestController("dns", `single argument`)
	if err := setup(c); err == nil {
		t.Errorf("Test 2, expected errors, but got none")
	}
}
