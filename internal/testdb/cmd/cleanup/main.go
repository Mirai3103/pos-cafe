package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := testdb.Cleanup(ctx, os.Getenv("TEST_DATABASE_URL")); err != nil {
		fmt.Fprintf(os.Stderr, "clean integration databases: %v\n", err)
		os.Exit(1)
	}
}
