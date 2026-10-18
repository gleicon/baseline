package audit

import "testing"

func TestSuggestForLongestPrefixWins(t *testing.T) {
	if got := SuggestFor("file:mode_mismatch:/etc/motd"); got == "" {
		t.Fatal("a known deviation must carry advice")
	}
	if SuggestFor("something:unknown:x") != "" {
		t.Error("an unknown deviation must not invent advice")
	}
}

// Advice explains the deviation list; repeating it per deviation would bury it.
func TestSuggestionsAreDistinct(t *testing.T) {
	devs := []string{
		"pkg:unauthorized:vim",
		"pkg:unauthorized:curl",
		"file:missing:/etc/motd",
	}
	got := Suggestions(devs)
	if len(got) != 2 {
		t.Fatalf("want 2 distinct suggestions for 3 deviations, got %d: %v", len(got), got)
	}
}
