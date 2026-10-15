// Administrator compatibility tool for old local installations. Product scan
// actions remain in Web; Agent never opens a database.
package main

import (
	"axiom/code-audit-server/internal/config"
	"axiom/code-audit-server/internal/store"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() { os.Exit(runEnqueue(os.Args[1:])) }
func runEnqueue(args []string) int {
	fs := flag.NewFlagSet("enqueue", flag.ExitOnError)
	source := fs.String("source", "", "source snapshot directory")
	provider := fs.String("provider", "openai", "provider")
	model := fs.String("model", "gpt-5.6-sol", "model")
	baseURL := fs.String("base-url", "", "optional base URL")
	mode := fs.String("mode", "standard", "standard|deep")
	timeout := fs.Int("timeout", 60, "timeout minutes")
	scanNo := fs.String("scan-no", "", "optional scan number")
	_ = fs.Parse(args)
	if *source == "" {
		fmt.Fprintln(os.Stderr, "enqueue --source <dir> is required")
		return 2
	}
	abs, err := filepath.Abs(*source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	st, err := store.Open(config.DatabaseSource(os.Getenv))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer st.Close()
	no := *scanNo
	if no == "" {
		no = fmt.Sprintf("SCAN-%d", os.Getpid())
	}
	id, err := st.InsertPending(context.Background(), store.Task{
		ScanNo:         no,
		SourcePath:     abs,
		Provider:       *provider,
		Model:          *model,
		BaseURL:        *baseURL,
		ScanMode:       *mode,
		TimeoutMinutes: *timeout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("enqueued id=%d scan_no=%s\n", id, no)
	return 0
}
