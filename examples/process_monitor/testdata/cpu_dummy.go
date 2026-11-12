// Command cpu_dummy burns a given percentage of one CPU core until stopped.
//
//	go run ./testdata/cpu_dummy.go 60
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const period = 100 * time.Millisecond

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cpu_dummy <percent>")
		os.Exit(2)
	}
	percent, err := strconv.Atoi(os.Args[1])
	if err != nil || percent < 1 || percent > 100 {
		fmt.Fprintln(os.Stderr, "percent must be an integer from 1 to 100")
		os.Exit(2)
	}

	fmt.Printf("cpu_dummy pid=%d: using %d%% of one core\n", os.Getpid(), percent)
	burn(percent)
}

func burn(percent int) {
	active := time.Duration(float64(period) * float64(percent) / 100)
	for {
		start := time.Now()
		for time.Since(start) < active {
		}
		if rest := period - time.Since(start); rest > 0 {
			time.Sleep(rest)
		}
	}
}
