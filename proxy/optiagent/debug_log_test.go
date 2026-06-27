package optiagent

import (
	"fmt"
	"regexp"
	"testing"
)

func TestDebugRegex(t *testing.T) {
	pattern := `(?m)^[ \t]*(?:File|at|\d+:|\S+\s*\(|.*\)\s*$|\S+\s*=|<unknown>)`
	re := regexp.MustCompile(pattern)
	cases := []string{
		"  File /app/x.py, line 1",
		"    at func (file:1:1)",
		"   6: <unknown>",
		"   0: std::panicking::panic::main",
		"   2: my_app::handle_request",
	}
	for _, c := range cases {
		fmt.Printf("match %q: %v\n", c, re.MatchString(c))
	}
}
