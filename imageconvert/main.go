package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type QueuedItem struct {
	Basename string   // Filename without Extension
	Filename string   // Filename with Extension
	Nest     []string // Subdirectories
}

const (
	OUTPUT_DIR  = "convert"
	OUTPUT_FLAG = 0755
)

var (
	featureResume     bool = true
	featureRecursive  bool = false
	featureSkipErrors bool = false
	flags             []string
	queue             []QueuedItem
	workers           = 1
)

func scan(nest []string, extensions []string) {
	if len(nest) == 1 && strings.EqualFold(nest[0], OUTPUT_DIR) {
		return
	}
	folder := path.Join(nest...)
	if folder == "" {
		folder = "."
	}
	files, err := os.ReadDir(folder)
	if err != nil {
		log.Fatalf("Error reading directory '%s': %s\n", folder, err)
	}
	for _, entry := range files {
		filename := entry.Name()

		// Scan Subdirectory
		if featureRecursive && entry.IsDir() {
			scan(append(nest, filename), extensions)
			continue
		}

		// Match File Extension
		for _, prefix := range extensions {
			if len(filename) < len(prefix) {
				continue
			}
			if !strings.EqualFold(filename[len(filename)-len(prefix):], prefix) {
				continue
			}

			// Perform Resume Check
			basename := filename[:strings.LastIndex(filename, ".")]
			location := fmt.Sprint(path.Join(OUTPUT_DIR, folder, basename), ".", flags[2])
			if featureResume {
				if info, err := os.Stat(location); err == nil {
					if info.Size() != 0 {
						fmt.Printf("Skipping '%s' as it is already complete\n", filename)
						continue
					}
				}
			}

			// Add Item to Queue
			queue = append(queue, QueuedItem{
				Filename: filename,
				Basename: basename,
				Nest:     nest,
			})
			break
		}
	}
}

func main() {
	t := time.Now()

	// Collect Arguments
	for _, arg := range os.Args {
		if strings.EqualFold(arg, "--skip-resume") {
			log.Println("Flag: Disabling Resume Check")
			featureResume = false
			continue
		}
		if strings.EqualFold(arg, "--recursive") {
			log.Println("Flag: Scanning Recursively")
			featureRecursive = true
			continue
		}
		if strings.EqualFold(arg, "--multithread") {
			log.Println("Flag: Enabling Multi-threading")
			workers = runtime.NumCPU()
			continue
		}
		if strings.EqualFold(arg, "--skip-errors") {
			log.Println("Flag: Skipping on Conversion Error")
			featureSkipErrors = true
			continue
		}
		flags = append(flags, arg)
	}
	if len(flags) < 3 {
		fmt.Println("imageconvert")
		fmt.Println("    --skip-errors   - Skip on conversion error")
		fmt.Println("    --skip-resume   - Skip Resume Checking")
		fmt.Println("    --multithread	 - Use Multiple Threads")
		fmt.Println("    --recursive     - Scan Directories Recursively")
		fmt.Println("    <From>          - File Extension(s) to convert from, delimited with comma")
		fmt.Println("    <To>            - File Extension to convert into")
		fmt.Println("    [Arguments]     - Arguments to pass onto ImageMagick")
		os.Exit(0)
	}

	// Scan Directory
	scan([]string{}, strings.Split(flags[1], ","))

	// Startup Workers
	var consoleLock sync.Mutex
	var awaitWorkers sync.WaitGroup
	var itemsRemaining atomic.Int32
	itemsRemaining.Add(int32(len(queue)))
	jobs := make(chan int, len(queue))

	log.Printf("Queued Files: %d\n", len(queue))
	log.Printf("Worker Count: %d\n", workers)

	for workerID := 0; workerID < workers; workerID++ {
		awaitWorkers.Add(1)
		go func() {
			defer awaitWorkers.Done()
			for i := range jobs {
				info := queue[i]

				// Generate Paths
				directory := path.Join(info.Nest...)
				srcPath := path.Join(directory, info.Filename)
				dstPath := fmt.Sprint(path.Join(OUTPUT_DIR, directory, info.Basename), ".", flags[2])

				if err := os.MkdirAll(path.Join(OUTPUT_DIR, directory), OUTPUT_FLAG); err != nil {
					log.Fatalln("Cannot create output directory:", err)
				}

				// Compile Arguments
				args := make([]string, 0, len(flags))
				args = append(args, srcPath)
				for i := 3; i < len(flags); i++ {
					args = append(args, flags[i])
				}
				args = append(args, dstPath)
				proc := exec.Command("magick", args...)

				// Output Errors
				if output, err := proc.CombinedOutput(); err != nil {

					consoleLock.Lock()
					exitcode := -1
					if proc.ProcessState != nil {
						exitcode = proc.ProcessState.ExitCode()
					}
					fmt.Printf("\r")
					log.Printf("Processing Failed for '%s' with code: %d\n%s\n\n",
						srcPath, exitcode, strings.TrimSpace(string(output)))
					consoleLock.Unlock()

					if !featureSkipErrors {
						os.Exit(1)
					}
				}

				// Output Progress
				consoleLock.Lock()
				fmt.Printf("\r                                                                                ")
				fmt.Printf("\rItems Left: %d", itemsRemaining.Add(-1))
				consoleLock.Unlock()
			}
		}()
	}

	// Begin Processing
	for i := 0; i < len(queue); i++ {
		jobs <- i
	}
	close(jobs)
	awaitWorkers.Wait()

	// Processing Complete
	fmt.Printf("\n")
	log.Printf("Processing Completed in %s\n", time.Since(t))
}
