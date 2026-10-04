package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

const usage = `usage:
  coldcat import <disk id> <path to checksum file>
`

func main() {
	args := os.Args[1:]

	if len(args) < 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(-1)
	}

	switch args[0] {
	case "import":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr,
				"expected a disk id and a path to the file to import for command `import`")
			fmt.Fprint(os.Stderr, usage)
			os.Exit(-1)
		}

		diskId, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid disk id %q: %v\n", args[1], err)
			os.Exit(-1)
		}

		db, err := database.Open("coldcat.sqlite")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
			os.Exit(-1)
		}
		defer db.Close()

		path := args[2]
		if err := app.New(db).Import(base.DiskId(diskId), path); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(-1)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(-1)
	}

	return
}
