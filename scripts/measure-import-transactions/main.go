package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const transactionWrapper = `// SetTransactionObserverForBenchmark exists only in the test overlay.
// The caller must serialize observer changes and transactions on this DB.
func (db *DB) SetTransactionObserverForBenchmark(observer func(time.Duration, error)) {
	db.transactionObserver = observer
}

func (db *DB) TransactionContext(ctx context.Context, fn func(*Tx) error) error {
	if db.transactionObserver == nil {
		return db.transactionContextUnobserved(ctx, fn)
	}
	started := time.Now()
	err := db.transactionContextUnobserved(ctx, fn)
	elapsed := time.Since(started)
	db.transactionObserver(elapsed, err)
	return err
}

func (db *DB) transactionContextUnobserved(ctx context.Context, fn func(*Tx) error) error {`

func run() error {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("locate transaction timing harness")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	original := filepath.Join(root, "internal", "database", "db.go")
	data, err := os.ReadFile(original)
	if err != nil {
		return err
	}
	source := string(data)
	for _, replacement := range []struct{ old, new string }{
		{`"sync"`, "\"sync\"\n\t\"time\""},
		{"type DB struct {", "type DB struct {\n\ttransactionObserver func(time.Duration, error)"},
		{
			"func (db *DB) TransactionContext(ctx context.Context, fn func(*Tx) error) error {",
			transactionWrapper,
		},
	} {
		if strings.Count(source, replacement.old) != 1 {
			return fmt.Errorf("transaction timing overlay anchor changed: %q", replacement.old)
		}
		source = strings.Replace(source, replacement.old, replacement.new, 1)
	}

	temp, err := os.MkdirTemp("/tmp/opencode", "transaction-timing-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	replacement := filepath.Join(temp, "db.go")
	if err := os.WriteFile(replacement, []byte(source), 0600); err != nil {
		return err
	}
	overlay, err := json.Marshal(map[string]any{
		"Replace": map[string]string{original: replacement},
	})
	if err != nil {
		return err
	}
	overlayPath := filepath.Join(temp, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		return err
	}
	args := []string{
		"test", "-overlay", overlayPath, "-tags", "transactiontiming", "./internal/importer",
	}
	command := exec.Command("go", append(args, os.Args[1:]...)...)
	command.Dir = root
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
