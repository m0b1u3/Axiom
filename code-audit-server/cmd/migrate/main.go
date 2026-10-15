package main

import (
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/store"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	source := flag.String("source", "", "SQLite source path (read only)")
	flag.Parse()
	dsn := os.Getenv("AXIOM_DATABASE_URL")
	if *source == "" || dsn == "" {
		fmt.Fprintln(os.Stderr, "--source and AXIOM_DATABASE_URL are required")
		os.Exit(1)
	}
	s, e := store.Open(dsn)
	if e != nil {
		panic(e)
	}
	defer s.Close()
	counts, e := s.ImportSQLite(context.Background(), *source)
	if e != nil {
		panic(e)
	}
	configs, e := s.ListModelConfigs(context.Background())
	if e != nil {
		panic(e)
	}
	for _, c := range configs {
		if c.ApiKeyEncrypted != "" {
			if _, e = cryptokey.Decrypt(c.ApiKeyEncrypted); e != nil {
				panic(fmt.Errorf("model config %d key cannot be decrypted: %w", c.ID, e))
			}
		}
	}
	b, _ := json.Marshal(counts)
	fmt.Println(string(b))
	fmt.Printf("verified %d model configurations; secret values not emitted\n", len(configs))
}
