package main

import (
	"fmt"
	"io"
)

// Only this exact, configuration-free invocation reports the compiled SQL
// requirement. Deployment still verifies binary provenance and actual DB ACLs.
func reportExecutionAccess(args []string, output io.Writer) bool {
	if len(args) != 1 || args[0] != "--execution-access-profile" {
		return false
	}
	fmt.Fprintln(output, `{"format":1,"role":"worker","profile":"worker-minimal-v1"}`)
	return true
}
