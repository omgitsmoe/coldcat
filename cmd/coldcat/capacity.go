package main

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var capacityPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z]*)$`)
var capacityUnits = map[string]int64{
	"": 1, "b": 1,
	"kb": 1_000, "mb": 1_000_000, "gb": 1_000_000_000, "tb": 1_000_000_000_000, "pb": 1_000_000_000_000_000,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40, "pib": 1 << 50,
}

func parseCapacity(value string) (int64, error) {
	parts := capacityPattern.FindStringSubmatch(strings.TrimSpace(value))
	if parts == nil {
		return 0, fmt.Errorf("expected a number followed by an optional byte unit")
	}

	unit, ok := capacityUnits[strings.ToLower(parts[2])]
	if !ok {
		return 0, fmt.Errorf("unknown byte unit %q", parts[2])
	}

	number, ok := new(big.Rat).SetString(parts[1])
	if !ok {
		return 0, fmt.Errorf("invalid size number %q", parts[1])
	}

	bytes := number.Mul(number, new(big.Rat).SetInt64(unit))
	if !bytes.IsInt() || !bytes.Num().IsInt64() {
		return 0, fmt.Errorf("capacity must be a whole number of bytes within the supported range")
	}

	return bytes.Num().Int64(), nil
}
