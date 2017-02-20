package tests

import (
	"fmt"
	"testing"
)

// succeedMark is the Unicode codepoint for a check mark.
const succeedMark = "\u2713"

// Passed logs the failure message using the giving message and values.
func Passed(t *testing.T, message string, val ...interface{}) {
	t.Logf("\t%s\t %s", succeedMark, fmt.Sprintf(message, val...))
}

// failedMark is the Unicode codepoint for an X mark.
const failedMark = "\u2717"

// Failed logs the failure message using the giving message and values.
func Failed(t *testing.T, message string, val ...interface{}) {
	t.Fatalf("\t%s\t %s", failedMark, fmt.Sprintf(message, val...))
}

// Errored logs the error message using the giving message and values.
func Errored(t *testing.T, message string, val ...interface{}) {
	t.Errorf("\t%s\t %s", failedMark, fmt.Sprintf(message, val...))
}
