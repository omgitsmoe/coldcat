package importer

import (
	"testing"
	"time"
)

func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("\ngot '%v'\nwant '%v'", got, want)
	}
}

func assertTimeApproxEqual(t *testing.T, got, want time.Time, tolerance time.Duration) {
	t.Helper()

	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}

	if diff > tolerance {
		t.Fatalf(
			"time mismatch: got %v, want %v (diff %v > %v)",
			got, want, diff, tolerance,
		)
	}
}

func assertSliceEqual[T comparable](t *testing.T, actual []T, expected []T) {
	t.Helper()

	if (actual == nil) != (expected == nil) {
		t.Fatalf("nil mismatch: expected %v, got %v", expected, actual)
	}

	if len(actual) != len(expected) {
		t.Logf("\nwant %v\n vs\n got %v", expected, actual)
		t.Fatalf(
			"expected len %d, got %d",
			len(expected), len(actual),
		)
	}

	for i := range expected {
		if expected[i] != actual[i] {
			t.Logf("\nwant %v\n vs\n got %v", expected, actual)
			t.Fatalf("at index %d: expected %v, got %v", i, expected[i], actual[i])
		}
	}
}

// assertFilesEqual compares field by field instead of with reflect.DeepEqual so
// that failures name the offending field, and so mtime can be compared with a
// tolerance: parseMTime goes through float64, which cannot represent an epoch
// second plus 7-digit nanoseconds exactly (1673815645.7979772 rounds to
// ...797977209ns).
func assertFilesEqual(t *testing.T, got, want []File) {
	t.Helper()

	if len(got) != len(want) {
		t.Logf("want %v vs got %v", want, got)
		t.Fatalf("expected len %d, got %d", len(want), len(got))
	}

	for i := range want {
		assertEqual(t, got[i].Name, want[i].Name)
		assertEqual(t, got[i].PathRelativeToRoot, want[i].PathRelativeToRoot)
		assertTimeApproxEqual(t, got[i].MTime, want[i].MTime, time.Microsecond)
		assertEqual(t, got[i].SizeInBytes, want[i].SizeInBytes)
		assertEqual(t, got[i].HashType, want[i].HashType)
		assertSliceEqual(t, got[i].Hash, want[i].Hash)
	}
}
