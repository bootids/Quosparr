package meter

import (
	"os"
	"slices"
	"testing"
)

func TestDisabledRoundTrip(t *testing.T) {
	root := t.TempDir()
	if off, err := IsDisabled(root, "minimax"); err != nil || off {
		t.Fatalf("missing file: %v %v", off, err)
	}
	for _, p := range []string{"minimax", "cursor", "minimax"} {
		if err := SetDisabled(root, p, true); err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := LoadDisabled(root); err != nil || !slices.Equal(ids, []string{"cursor", "minimax"}) {
		t.Fatalf("ids %v %v", ids, err)
	}
	if err := SetDisabled(root, "minimax", false); err != nil {
		t.Fatal(err)
	}
	if off, _ := IsDisabled(root, "minimax"); off {
		t.Fatal("minimax still disabled")
	}
	if fi, err := os.Stat(DisabledPath(root)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm: %v %v", fi.Mode(), err)
	}
}

func TestDisabledCorruptFileIsError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(DisabledPath(root), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := IsDisabled(root, "minimax"); err == nil {
		t.Fatal("want decode error")
	}
}
