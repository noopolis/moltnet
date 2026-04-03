package protocol

import "testing"

func TestUniqueTrimmedStrings(t *testing.T) {
	t.Parallel()

	values := UniqueTrimmedStrings([]string{" writer ", "", "writer", "reviewer", "reviewer "})
	if len(values) != 2 || values[0] != "writer" || values[1] != "reviewer" {
		t.Fatalf("unexpected unique values %#v", values)
	}
}

func TestSortedUniqueTrimmedStrings(t *testing.T) {
	t.Parallel()

	values := SortedUniqueTrimmedStrings([]string{" writer ", "alpha", "writer", "beta", "alpha"})
	if len(values) != 3 || values[0] != "alpha" || values[1] != "beta" || values[2] != "writer" {
		t.Fatalf("unexpected sorted unique values %#v", values)
	}
}

func TestIsKnownPartKind(t *testing.T) {
	t.Parallel()

	if !IsKnownPartKind(PartKindImage) {
		t.Fatal("expected known part kind")
	}
	if IsKnownPartKind("mystery") {
		t.Fatal("expected unknown part kind to be rejected")
	}
}
