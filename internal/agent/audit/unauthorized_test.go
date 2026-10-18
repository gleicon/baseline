package audit

import "testing"

func TestUnauthorizedSet(t *testing.T) {
	got := unauthorizedSet([]string{"bash", "openssh-server"}, []string{"bash", "curl", "openssh-server", "vim"})
	want := []string{"curl", "vim"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(unauthorizedSet([]string{"a", "b"}, []string{"a", "b"})) != 0 {
		t.Fatal("expected no unauthorized entries for identical sets")
	}
}
