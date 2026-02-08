package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joelklabo/agentpay/cmd"
)

func main() {
	ctx := context.Background()
	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
