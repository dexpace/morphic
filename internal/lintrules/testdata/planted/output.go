package planted

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
)

func plantOutput(w io.Writer, logger *slog.Logger) {
	fmt.Println("x")                  // want "use of `fmt.Println` forbidden by pattern `^fmt\.Print(f|ln)?$` (forbidigo)"
	fmt.Printf("%s", "x")             // want "use of `fmt.Printf` forbidden by pattern `^fmt\.Print(f|ln)?$` (forbidigo)"
	fmt.Fprintln(os.Stderr, "x")      // want "use of `os.Stderr` forbidden by pattern `^os\.(Stdout|Stderr)$` (forbidigo)"
	_, _ = os.Stdout.WriteString("x") // want "use of `os.Stdout` forbidden by pattern `^os\.(Stdout|Stderr)$` (forbidigo)"
	println("x")                      // want "use of `println` forbidden by pattern `^print(ln)?$` (forbidigo)"
	log.Printf("%s", "x")             // want "use of `log.Printf` forbidden by pattern `^log\.` (forbidigo)"
	slog.Info("x")                    // want "use of `slog.Info` forbidden by pattern `^slog\.(Default|SetDefault|Debug|DebugContext|Info|InfoContext|Warn|WarnContext|Error|ErrorContext|Log|LogAttrs)$` (forbidigo)"
	_ = slog.Default()                // want "use of `slog.Default` forbidden by pattern `^slog\.(Default|SetDefault|Debug|DebugContext|Info|InfoContext|Warn|WarnContext|Error|ErrorContext|Log|LogAttrs)$` (forbidigo)"
	logger.Info("x")
	_, _ = fmt.Fprintf(w, "%s", fmt.Sprint("x"))
}
