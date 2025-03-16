package grog

import (
	"fmt"
	"sync"
	"time"
)

const numWorkers = 5

// OutputTest Try to emulate the way bazel outputs status updates to the terminal
func OutputTest() {
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Clear screen & print persistent header
	fmt.Print("\033[H\033[J") // Clear terminal
	fmt.Println("=== My Bazel-Like Build System ===")
	fmt.Println("Status Updates:")

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j <= 100; j += 20 {
				// Move cursor below header to specific worker line
				fmt.Printf("\033[%d;0H\033[KWorker %d: %d%% complete\n", id+3, id, j)
				time.Sleep(time.Millisecond * 500)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("\033[%d;0H\033[KAll tasks complete!\n", numWorkers+3)
}
