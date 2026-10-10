package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

const importRSSChildEnv = "COLDCAT_IMPORT_RSS_CHILD"

type importRSSRequest struct {
	Input   string
	Catalog string
	Result  string
	Files   int
	Fail    bool
}

type importRSSResult struct {
	Version        int
	Snapshot       base.Snapshot
	ImportError    string
	CommittedFiles int64
	ImportDuration time.Duration
	PostCloseHWM   int64
}

type importRSSSample struct {
	result   importRSSResult
	maxBytes int64
	lifetime int64
	elapsed  time.Duration
	userCPU  time.Duration
	sysCPU   time.Duration
}

func decodeImportRSSJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected data after RSS protocol object: %v", err)
	}
	return nil
}

func TestImportRSSChild(t *testing.T) {
	if os.Getenv(importRSSChildEnv) != "1" {
		t.Skip("invoked only by the isolated RSS parent")
	}
	data, err := io.ReadAll(os.Stdin)
	assertNoErr(t, err)
	var request importRSSRequest
	assertNoErr(t, decodeImportRSSJSON(data, &request))
	if request.Files <= 0 || request.Files%defaultBatchSize != 0 ||
		request.Input == "" || request.Catalog == "" || request.Result == "" {
		t.Fatal("invalid RSS child request")
	}
	for _, path := range []string{request.Catalog, request.Result} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("RSS child output must not exist: %v", err)
		}
	}
	db, err := database.OpenContext(t.Context(), request.Catalog)
	assertNoErr(t, err)
	t.Cleanup(func() { db.Close() })
	disk, err := db.CreateDisk("disk", "", "", 0)
	assertNoErr(t, err)
	result := importRSSResult{Version: 1}
	started := time.Now()
	result.Snapshot, err = Import(t.Context(), db, Request{
		DiskID: base.DiskId(disk), Path: request.Input, CapturedAt: time.Unix(10, 0),
		Progress: func(p Progress) error {
			result.CommittedFiles = p.CommittedFiles
			return nil
		},
	})
	result.ImportDuration = time.Since(started)
	if err != nil {
		result.ImportError = err.Error()
	}
	assertNoErr(t, validateImportRSSResult(request, result))
	assertNoErr(t, db.Close())
	status, err := os.ReadFile("/proc/self/status")
	assertNoErr(t, err)
	result.PostCloseHWM, err = parseImportRSSHWM(string(status))
	assertNoErr(t, err)
	data, err = json.Marshal(result)
	assertNoErr(t, err)
	assertNoErr(t, os.WriteFile(request.Result, data, 0600))
}

func parseImportRSSHWM(status string) (int64, error) {
	var peak int64
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "VmHWM:" {
			continue
		}
		if peak != 0 || len(fields) != 3 || fields[2] != "kB" {
			return 0, fmt.Errorf("invalid Linux VmHWM: %q", line)
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kib <= 0 || kib > (1<<63-1)/1024 {
			return 0, fmt.Errorf("invalid Linux VmHWM amount: %q", line)
		}
		peak = kib * 1024
	}
	if peak == 0 {
		return 0, fmt.Errorf("missing Linux VmHWM")
	}
	return peak, nil
}

func validateImportRSSResult(request importRSSRequest, result importRSSResult) error {
	if result.Version != 1 || result.ImportDuration <= 0 ||
		result.CommittedFiles != int64(request.Files) {
		return fmt.Errorf("invalid RSS completion: %+v", result)
	}
	if request.Fail {
		line := fmt.Sprintf("line %d:", request.Files+1)
		if !strings.HasPrefix(result.ImportError, "parse ") ||
			!strings.Contains(result.ImportError, line) ||
			strings.Contains(result.ImportError, "cleanup snapshot") || result.Snapshot.Id != 0 {
			return fmt.Errorf("unexpected late-failure result: %+v", result)
		}
	} else if result.ImportError != "" || result.Snapshot.Id <= 0 ||
		result.Snapshot.FileCount != int64(request.Files) ||
		result.Snapshot.ContentCount != int64(request.Files) {
		return fmt.Errorf("unexpected successful result: %+v", result)
	}
	return nil
}

func runImportRSSChild(ctx context.Context, request importRSSRequest) (importRSSSample, error) {
	var sample importRSSSample
	executable, err := os.Executable()
	if err != nil {
		return sample, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return sample, err
	}
	command := exec.CommandContext(ctx, executable,
		"-test.run=^TestImportRSSChild$", "-test.timeout=30m")
	command.Env = append(os.Environ(), importRSSChildEnv+"=1")
	command.Stdin = bytes.NewReader(data)
	started := time.Now()
	output, err := command.CombinedOutput()
	sample.elapsed = time.Since(started)
	if err != nil {
		return sample, fmt.Errorf("RSS child failed: %w\n%s", err, output)
	}
	usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss <= 0 || usage.Maxrss > (1<<63-1)/1024 {
		return sample, fmt.Errorf("missing Linux child maximum RSS")
	}
	// Keep lifetime accounting separate from the post-exec address-space high-water mark.
	sample.lifetime = usage.Maxrss * 1024
	sample.userCPU = command.ProcessState.UserTime()
	sample.sysCPU = command.ProcessState.SystemTime()
	data, err = os.ReadFile(request.Result)
	if err != nil {
		return sample, fmt.Errorf("missing RSS completion: %w", err)
	}
	if err := decodeImportRSSJSON(data, &sample.result); err != nil {
		return sample, err
	}
	if sample.result.PostCloseHWM <= 0 {
		return sample, fmt.Errorf("missing child post-close RSS high-water mark")
	}
	sample.maxBytes = sample.result.PostCloseHWM
	return sample, validateImportRSSResult(request, sample.result)
}

func checkImportRSSCatalogBeforeRecovery(
	ctx context.Context, request importRSSRequest, result importRSSResult,
) error {
	// Read-only inspection precedes application open so recovery cannot mask failed cleanup.
	raw, err := sql.Open("sqlite", request.Catalog+"?mode=ro")
	if err != nil {
		return err
	}
	defer raw.Close()
	if request.Fail {
		for _, table := range []string{
			"snapshot", "observation", "content", "pending_size", "import_content",
			"search_path", "search_trigram", "directory", "directory_content",
			"directory_file", "directory_build",
		} {
			var count int
			if err := raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("RSS cleanup left %d rows in %s", count, table)
			}
		}
		return nil
	}
	var total, complete int
	if err := raw.QueryRowContext(ctx,
		`SELECT COUNT(*),COUNT(CASE WHEN id=? AND state='complete' THEN 1 END)
 FROM snapshot`, result.Snapshot.Id).Scan(&total, &complete); err != nil {
		return err
	}
	if total != 1 || complete != 1 {
		return fmt.Errorf("RSS publication left %d snapshots, %d expected complete", total, complete)
	}
	return nil
}

func assertImportRSSCatalog(tb testing.TB, request importRSSRequest, sample importRSSSample) {
	tb.Helper()
	assertNoErr(tb, checkImportRSSCatalogBeforeRecovery(tb.Context(), request, sample.result))
	db, err := database.OpenContext(tb.Context(), request.Catalog)
	assertNoErr(tb, err)
	defer db.Close()
	var importErr error
	if sample.result.ImportError != "" {
		importErr = errors.New(sample.result.ImportError)
	}
	assertIndexedImport(tb, db, request.Catalog, request.Files, request.Fail,
		sample.result.Snapshot, importErr)
}

func newImportRSSRequest(tb testing.TB, input string, files int, fail bool) importRSSRequest {
	tb.Helper()
	dir := tb.TempDir()
	return importRSSRequest{
		Input: input, Catalog: filepath.Join(dir, "catalog.sqlite"),
		Result: filepath.Join(dir, "result.json"), Files: files, Fail: fail,
	}
}

func TestImportPeakRSSFixture(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_failure_%t", fail), func(t *testing.T) {
			const files = 2 * defaultBatchSize
			request := newImportRSSRequest(t, writeIndexedImportFixture(t, files, fail), files, fail)
			sample, err := runImportRSSChild(t.Context(), request)
			assertNoErr(t, err)
			assertImportRSSCatalog(t, request, sample)
		})
	}
}

func TestImportRSSChildErrors(t *testing.T) {
	for _, invalidCount := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_count_%t", invalidCount), func(t *testing.T) {
			request := newImportRSSRequest(t, filepath.Join(t.TempDir(), "missing.cshd"),
				defaultBatchSize, false)
			if invalidCount {
				request.Files = 0
			}
			if _, err := runImportRSSChild(t.Context(), request); err == nil {
				t.Fatal("invalid child operation succeeded")
			}
			if _, err := os.Stat(request.Result); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed child produced completion: %v", err)
			}
		})
	}
}

func TestImportRSSProtocol(t *testing.T) {
	for _, data := range []string{`{}`, `{"Version":1,"unknown":true}`, `{} {}`, `broken`} {
		var result importRSSResult
		err := decodeImportRSSJSON([]byte(data), &result)
		if err == nil {
			err = validateImportRSSResult(importRSSRequest{Files: defaultBatchSize}, result)
		}
		if err == nil {
			t.Fatalf("invalid completion accepted: %s", data)
		}
	}
}

func TestImportRSSUncleanCatalog(t *testing.T) {
	request := newImportRSSRequest(t, "unused.cshd", defaultBatchSize, true)
	db, err := database.OpenContext(t.Context(), request.Catalog)
	assertNoErr(t, err)
	t.Cleanup(func() { db.Close() })
	disk, err := db.CreateDisk("disk", "", "", 0)
	assertNoErr(t, err)
	assertNoErr(t, db.TransactionContext(t.Context(), func(tx *database.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO snapshot(
 disk_id,state,captured_at,imported_at,capture_provenance,input_path,input_format)
 VALUES(?,'importing','2026-10-09T00:00:00Z','2026-10-09T00:00:00Z','explicit','fixture','cshd')`, disk)
		return err
	}))
	assertNoErr(t, db.Close())
	for range 2 {
		err := checkImportRSSCatalogBeforeRecovery(t.Context(), request, importRSSResult{})
		if err == nil || !strings.Contains(err.Error(), "1 rows in snapshot") {
			t.Fatalf("unclean snapshot accepted or recovered: %v", err)
		}
	}
}

func TestImportRSSHWM(t *testing.T) {
	peak, err := parseImportRSSHWM("Name:\tchild\nVmHWM:\t123 kB\nVmRSS:\t99 kB\n")
	assertNoErr(t, err)
	if peak != 123*1024 {
		t.Fatalf("VmHWM bytes: %d", peak)
	}
	for _, status := range []string{
		"", "VmRSS: 123 kB", "VmHWM: 0 kB", "VmHWM: -1 kB", "VmHWM: 123 MB",
		"VmHWM: 123", "VmHWM: nope kB", "VmHWM: 9223372036854775807 kB",
		"VmHWM: 123 kB\nVmHWM: 124 kB",
	} {
		if _, err := parseImportRSSHWM(status); err == nil {
			t.Fatalf("invalid VmHWM accepted: %q", status)
		}
	}
}

func BenchmarkImportPeakRSS(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		for _, fail := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/late_failure_%t", files, fail), func(b *testing.B) {
				input := writeIndexedImportFixture(b, files, fail)
				var sum, maximum, lifetimeMax int64
				var importTime, childTime, userCPU, sysCPU time.Duration
				for b.Loop() {
					b.StopTimer()
					request := newImportRSSRequest(b, input, files, fail)
					b.StartTimer()
					sample, err := runImportRSSChild(b.Context(), request)
					b.StopTimer()
					assertNoErr(b, err)
					assertImportRSSCatalog(b, request, sample)
					sum += sample.maxBytes
					maximum = max(maximum, sample.maxBytes)
					lifetimeMax = max(lifetimeMax, sample.lifetime)
					importTime += sample.result.ImportDuration
					childTime += sample.elapsed
					userCPU += sample.userCPU
					sysCPU += sample.sysCPU
					b.Logf("sample VmHWM=%d lifetimeRSS=%d bytes "+
						"import=%.6fs child=%.6fs user=%.6fs system=%.6fs",
						sample.maxBytes, sample.lifetime,
						sample.result.ImportDuration.Seconds(), sample.elapsed.Seconds(),
						sample.userCPU.Seconds(), sample.sysCPU.Seconds())
					b.StartTimer()
				}
				b.ReportMetric(float64(maximum), "child-max-rss-bytes")
				b.ReportMetric(float64(lifetimeMax), "child-lifetime-max-rss-bytes")
				b.ReportMetric(float64(sum)/float64(b.N), "child-mean-max-rss-bytes")
				b.ReportMetric(importTime.Seconds()/float64(b.N), "import-s/op")
				b.ReportMetric(childTime.Seconds()/float64(b.N), "child-s/op")
				b.ReportMetric(userCPU.Seconds()/float64(b.N), "child-user-cpu-s/op")
				b.ReportMetric(sysCPU.Seconds()/float64(b.N), "child-system-cpu-s/op")
			})
		}
	}
}
