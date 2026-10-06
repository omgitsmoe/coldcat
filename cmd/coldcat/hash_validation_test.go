package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestImportRejectsWrongHashLengthWithoutSuccessOutput(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			path, file := importFixture(t, 0)
			if err := os.WriteFile(file, []byte(",sha256,ab invalid\n"), 0600); err != nil {
				t.Fatal(err)
			}
			extra := []string{"--disk-id", "1"}
			if jsonMode {
				extra = append(extra, "--json")
			}
			cmd := newCommand()
			var stdout, stderr bytes.Buffer
			cmd.Writer, cmd.ErrWriter = &stdout, &stderr
			err := cmd.Run(t.Context(), importArgs(path, file, extra...))
			if !errors.Is(err, database.ErrValidation) || stdout.Len() != 0 {
				t.Fatalf("stdout=%q error=%v", stdout.String(), err)
			}
			for _, detail := range []string{"line 1", `path "invalid"`, "must be 32 bytes, got 1"} {
				if !strings.Contains(err.Error(), detail) {
					t.Fatalf("missing %q: %v", detail, err)
				}
			}
			db, err := database.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.LatestCompleteSnapshot(t.Context(), 1); !errors.Is(err, database.ErrNotFound) {
				t.Fatalf("publication: %v", err)
			}
		})
	}
}
