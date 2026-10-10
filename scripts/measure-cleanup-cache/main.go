package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const cacheWrapper = `
type CleanupCacheResult struct {
 Previous, Applied, Restored, MaxOpenConnections int
}

// CleanupImportWithCacheForBenchmark exists only in the generated test overlay.
// Pool limits stay at one until this fixture's handle closes; cache settings are restored.
func (db *DB) CleanupImportWithCacheForBenchmark(
 ctx context.Context, id int64, kib int,
) (result CleanupCacheResult, returned error) {
 if kib!=0 && kib!=8192 && kib!=32768 {
  return result, fmt.Errorf("unsupported diagnostic cache size: %d KiB", kib)
 }
 db.db.SetMaxOpenConns(1)
 db.db.SetMaxIdleConns(1)
 result.MaxOpenConnections=db.db.Stats().MaxOpenConnections
 conn, err:=db.db.Conn(ctx)
 if err!=nil { return result, err }
 defer func() { returned=errors.Join(returned,conn.Close()) }()
 if err:=conn.QueryRowContext(ctx,"PRAGMA cache_size").Scan(&result.Previous); err!=nil {
  return result,err
 }
 // Restore on the pinned connection after commit/rollback, even if the caller canceled.
 defer func() {
  restoreCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
  defer cancel()
  _,err:=conn.ExecContext(restoreCtx,fmt.Sprintf("PRAGMA cache_size=%d",result.Previous))
  if err==nil {
   err=conn.QueryRowContext(restoreCtx,"PRAGMA cache_size").Scan(&result.Restored)
  }
  if err==nil && result.Restored!=result.Previous {
   err=fmt.Errorf("cache restoration mismatch: %d, want %d",result.Restored,result.Previous)
  }
  if err!=nil { returned=errors.Join(returned,fmt.Errorf("restore diagnostic cache: %w",err)) }
 }()
 tx,err:=conn.BeginTx(ctx,nil)
 if err!=nil { return result,err }
 defer tx.Rollback()
 expected:=result.Previous
 if kib!=0 {
  expected=-kib
  if _,err:=tx.ExecContext(ctx,fmt.Sprintf("PRAGMA cache_size=%d",expected)); err!=nil {
   return result,err
  }
 }
 if err:=tx.QueryRowContext(ctx,"PRAGMA cache_size").Scan(&result.Applied); err!=nil {
  return result,err
 }
 if result.Applied!=expected {
  return result,fmt.Errorf("diagnostic cache not applied: %d, want %d",result.Applied,expected)
 }
 if err:=db.cleanupImportTxForBenchmark(ctx,&Tx{tx},id); err!=nil {
  return result,classifyError(err)
 }
 return result,classifyError(tx.Commit())
}
`

func overlaySource(source string) (string, error) {
	start := "func (db *DB) CleanupImport(ctx context.Context, id int64) error {\n" +
		"\treturn db.TransactionContext(ctx, func(tx *Tx) error {\n"
	end := "\n\t})\n}\n\ntype PublishImportRequest"
	if strings.Count(source, start) != 1 || strings.Count(source, end) != 1 {
		return "", fmt.Errorf("cleanup cache overlay anchors changed")
	}
	before, remaining, _ := strings.Cut(source, start)
	body, after, _ := strings.Cut(remaining, end)
	replacement := `func (db *DB) CleanupImport(ctx context.Context, id int64) error {
 return db.TransactionContext(ctx,func(tx *Tx) error {
  return db.cleanupImportTxForBenchmark(ctx,tx,id)
 })
}
func (db *DB) cleanupImportTxForBenchmark(ctx context.Context,tx *Tx,id int64) error {
` + body + "\n}\n" + cacheWrapper + "\ntype PublishImportRequest" + after
	result := before + replacement
	if strings.Count(result, `"fmt"`) != 1 {
		return "", fmt.Errorf("cleanup cache import anchor changed")
	}
	result = strings.Replace(result, `"fmt"`, "\"fmt\"\n\t\"time\"", 1)
	formatted, err := format.Source([]byte(result))
	return string(formatted), err
}

func run() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("cleanup cache diagnostic requires Linux RSS measurements")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("locate cleanup cache harness")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	original := filepath.Join(root, "internal", "database", "imports.go")
	data, err := os.ReadFile(original)
	if err != nil {
		return err
	}
	source, err := overlaySource(string(data))
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp("/tmp/opencode", "cleanup-cache-overlay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	replacement := filepath.Join(temp, "imports.go")
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
		"test", "-overlay", overlayPath, "-tags", "cleanupcache", "./internal/importer",
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
