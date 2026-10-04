package coldcat

import (
	"fmt"
	"os"
)

func main() int {
	args := os.Args[1:]

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "missing command, got args:", args)
		return -1
	}

	switch args[1] {
	case "import":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "expected a path to the file to import for command `import`")
			return -1
		}
		break
	}

	return 0
}
