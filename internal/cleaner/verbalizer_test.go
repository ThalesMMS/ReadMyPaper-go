package cleaner

import (
	"strings"
	"testing"
)

func TestVerbalizeScientificNotation(t *testing.T) {
	got := Verbalize("A 224×224 CT image reached 98.5% accuracy, p<0.05 and 4±1 mm.")
	for _, expected := range []string{"224 by 224", "C.T.", "98.5 percent", "p less than 0.05", "4 plus or minus 1"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("%q does not contain %q", got, expected)
		}
	}
}
