// Command healthcheck is a dependency-free container readiness probe.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := flag.String("url", os.Getenv("AQUAOS_HEALTHCHECK_URL"), "readiness endpoint")
	flag.Parse()
	if *url == "" {
		fail(fmt.Errorf("healthcheck URL is required through -url or AQUAOS_HEALTHCHECK_URL"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		fail(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		fail(fmt.Errorf("readiness returned %s", response.Status))
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
