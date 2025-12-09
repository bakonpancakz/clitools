package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
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
	featureResume    bool = true
	featureRecursive bool = false
	logLines         []string
	logMutex         sync.Mutex
	flags            []string
	queue            []QueuedItem
	itemsRemaining   atomic.Int32
	awaitWorkers     sync.WaitGroup
	workers          = 1
)

func defaultField(m map[string]string, key string) string {
	if val, ok := m[key]; ok {
		return val
	} else {
		return ""
	}
}

// Warning, magic numbers... really janky...

func logUpdate(index int, message string) {
	logMutex.Lock()
	logLines[index+1] = fmt.Sprintf("[%02d] %s", index, message)
	logMutex.Unlock()
}

func logRenderer() {
	logLines = make([]string, workers+3)
	for i := 0; i < len(logLines); i++ {
		fmt.Println()
	}
	for {
		logMutex.Lock()

		// Footer
		logLines[len(logLines)-1] = fmt.Sprintf("\rItems Left: %d\n", itemsRemaining.Load())

		// Log Lines
		fmt.Printf("\033[%dA", len(logLines)+1)
		for _, line := range logLines {
			fmt.Printf("\033[2K\r%-80s\n", line)
		}
		fmt.Printf("\r")

		logMutex.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

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
		if entry.IsDir() {
			if featureRecursive {
				scan(append(nest, filename), extensions)
			}
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

	// Enable ANSI escape codes on Windows 10+
	// 	I don't remember where I copied this from, sorry... (>_>)
	if runtime.GOOS == "windows" {
		stdout := os.Stdout.Fd()
		var mode uint32
		proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleMode")
		proc.Call(stdout, uintptr(unsafe.Pointer(&mode)))
		mode |= 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING
		proc = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
		proc.Call(stdout, uintptr(mode))
	}

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
		flags = append(flags, arg)
	}
	if len(flags) < 3 {
		fmt.Println("mediaconvert")
		fmt.Println("    --skip-resume    - Skip Resume Checking")
		fmt.Println("    --multithread    - Use Multiple Threads")
		fmt.Println("    --recursive      - Scan Directories Recursively")
		fmt.Println("    <From>           - File Extension(s) to convert from, delimited with comma")
		fmt.Println("    <To>             - File Extension to convert into")
		fmt.Println("    [Arguments]      - Arguments to pass onto FFMPEG")
		fmt.Println("Templates:")
		fmt.Println("    {filename}       - Full Filename    (e.g. myfile.txt)")
		fmt.Println("    {basename}       - Base Filename    (e.g. myfile")
		fmt.Println("    {directory}      - Source Directory (e.g. /path/to/file)")
		os.Exit(0)
	}

	// Scan Directory
	scan([]string{}, strings.Split(flags[1], ","))

	// Startup Workers
	log.Printf("Queued Files: %d\n", len(queue))
	log.Printf("Worker Count: %d\n", workers)
	jobs := make(chan int, len(queue))
	itemsRemaining.Add(int32(len(queue)))

	if len(queue) > 0 {
		go logRenderer()
	}

	for i := 0; i < workers; i++ {
		awaitWorkers.Add(1)
		go func(workerID int) {
			defer awaitWorkers.Done()
			for i := range jobs {
				info := queue[i]
				s := time.Now()

				// Generate Paths
				directory := path.Join(info.Nest...)
				srcPath := path.Join(directory, info.Filename)
				dstPath := fmt.Sprint(path.Join(OUTPUT_DIR, directory, info.Basename), ".", flags[2])

				if err := os.MkdirAll(path.Join(OUTPUT_DIR, directory), OUTPUT_FLAG); err != nil {
					log.Fatalln("Cannot create output directory:", err)
				}

				// Compile Arguments
				args := []string{"-hide_banner", "-y", "-progress", "-", "-i", srcPath}
				for i := 3; i < len(flags); i++ {
					str := flags[i]
					str = strings.ReplaceAll(str, "{basename}", info.Basename)
					str = strings.ReplaceAll(str, "{filename}", info.Filename)
					str = strings.ReplaceAll(str, "{directory}", path.Join(info.Nest...))
					args = append(args, str)
				}
				args = append(args, dstPath)
				proc := exec.Command("ffmpeg", args...)

				// Collect Error Output
				errors := bytes.Buffer{}
				stderr, err := proc.StderrPipe()
				if err != nil {
					log.Fatalf("Failed to open Error Output: %s\n", err)
				}
				go func() {
					scanner := bufio.NewScanner(stderr)
					for scanner.Scan() {
						errors.Write(scanner.Bytes())
						errors.WriteRune('\n')
					}
				}()

				// Collect Progress Output
				output, err := proc.StdoutPipe()
				if err != nil {
					log.Fatalf("Failed to open Standard Output: %s\n", err)
				}
				go func() {
					for {
						buffer := make([]byte, 256)
						r, err := output.Read(buffer)
						if err != nil {
							break
						}
						progress := map[string]string{}
						metadata := strings.Split(string(buffer[:r]), "\n")
						for _, line := range metadata {
							values := strings.Split(line, "=")
							if len(values) == 2 {
								key := strings.TrimSpace(values[0])
								val := strings.TrimSpace(values[1])
								progress[key] = val
							}
						}
						switch defaultField(progress, "progress") {
						case "continue":
							// Clear Output and Display Progress
							sizeTotal := defaultField(progress, "total_size")
							sizeFloat, _ := strconv.ParseFloat(sizeTotal, 64)
							sizeValue := strconv.FormatFloat(sizeFloat/1024/1024, 'f', 2, 64)
							logUpdate(workerID, fmt.Sprintf(
								"Time: %s, Bitrate: %s, FPS: %s/%s/%s, Size: %sMB (%s)",
								defaultField(progress, "out_time"),
								defaultField(progress, "bitrate"),
								defaultField(progress, "fps"),
								defaultField(progress, "drop_frames"),
								defaultField(progress, "dup_frames"),
								sizeValue,
								defaultField(progress, "speed"),
							))
						case "end":
							// Clear Output and Display Completion Time
							logUpdate(workerID, fmt.Sprintf(
								"Processing Completed in %s",
								time.Since(s),
							))
						}
					}
				}()

				// Start Processing
				if err := proc.Run(); err != nil {
					exitcode := -1
					if proc.ProcessState != nil {
						exitcode = proc.ProcessState.ExitCode()
					}
					log.Printf("Processing Failed for '%s' with code: %d\n%s\n\n",
						srcPath, exitcode, errors.String())
					os.Exit(1)
				}
				itemsRemaining.Add(-1)
			}
		}(i)
	}

	// Begin Processing
	for i := 0; i < len(queue); i++ {
		jobs <- i
	}
	close(jobs)
	awaitWorkers.Wait()

	// Processing Complete
	log.Printf("Processing Completed in %s\n", time.Since(t))
}
