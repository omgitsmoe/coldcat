package main

import "testing"

func TestParseCapacity(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "512", want: 512},
		{input: "512 B", want: 512},
		{input: "2TB", want: 2_000_000_000_000},
		{input: "2 tb", want: 2_000_000_000_000},
		{input: "1.5GB", want: 1_500_000_000},
		{input: "2TiB", want: 2 << 40},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := parseCapacity(test.input)
			if err != nil {
				t.Fatalf("parseCapacity(%q): %v", test.input, err)
			}

			if got != test.want {
				t.Fatalf("parseCapacity(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestParseCapacityRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "-1GB", "1.1B", "1XB", "9223372036854775808"} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseCapacity(input); err == nil {
				t.Fatalf("parseCapacity(%q) succeeded; want an error", input)
			}
		})
	}
}
