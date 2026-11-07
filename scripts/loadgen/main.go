// scripts/loadgen — load generator for Tailflow (design §18.5).
//
// Drives fakesource with 200 hosts, 50 containers each, a 2s metrics
// interval, and 10 simulated browser sessions. Runs for 30 minutes (or the
// duration passed via --duration).
//
// Pass criteria (§18.5):
//   - Resident memory at most 150 MB
//   - p95 API latency at most 100 ms
//   - Goroutine count flat (no growth over the last 20 minutes)
//   - Zero dropped structural events
//
// This is a stub — the actual implementation needs a running Tailflow server
// in local mode with a scripted fakesource fleet. The fakesource would need
// to be extended to support 200 hosts with periodic metric samples. That
// infrastructure lands with the integration test harness in M5.
//
// Usage:
//   go run ./scripts/loadgen --duration 30m
package main

import (
	"flag"
	"fmt"
	"time"
)

func main() {
	duration := flag.Duration("duration", 30*time.Minute, "how long to run the load test")
	hosts := flag.Int("hosts", 200, "number of simulated hosts")
	containers := flag.Int("containers", 50, "containers per host")
	browsers := flag.Int("browsers", 10, "simulated browser sessions")
	flag.Parse()

	fmt.Printf("Tailflow load generator (M4 stub)\n")
	fmt.Printf("  duration:   %s\n", duration)
	fmt.Printf("  hosts:      %d\n", *hosts)
	fmt.Printf("  containers: %d per host\n", *containers)
	fmt.Printf("  browsers:   %d\n", *browsers)
	fmt.Printf("\n")
	fmt.Printf("This is a stub. The actual load test needs:\n")
	fmt.Printf("  1. A running Tailflow server in local mode (TAILFLOW_MODE=local)\n")
	fmt.Printf("  2. A fakesource fleet scripted with %d hosts emitting metrics every 2s\n", *hosts)
	fmt.Printf("  3. %d concurrent HTTP clients hitting the API\n", *browsers)
	fmt.Printf("  4. A pprof endpoint to measure RSS and goroutine count\n")
	fmt.Printf("\n")
	fmt.Printf("The full implementation lands with the integration test harness in M5.\n")
	fmt.Printf("For now, this stub documents the contract per design §18.5.\n")
}
