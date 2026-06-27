package optiagent

import (
	"fmt"
	"testing"
)

func TestDebugIsToolMessage(t *testing.T) {
	// With escaped newlines (the test case)
	obj1 := []byte(`{"role":"tool","content":"Traceback"}`)
	fmt.Printf("obj1 isToolMessage=%v\n", isToolMessage(obj1))

	// With raw newlines (the input literal in the source)
	obj2 := []byte("{\"role\":\"tool\",\"content\":\"Traceback\"}")
	fmt.Printf("obj2 isToolMessage=%v\n", isToolMessage(obj2))

	// What the test actually passes
	test := `{"messages":[{"role":"tool","content":"` + "\n" + `"}]}`
	fmt.Printf("test payload: %q\n", test)
}
