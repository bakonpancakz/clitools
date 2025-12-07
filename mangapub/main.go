package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"os"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

type File struct {
	Name   string
	Images []Image
}

type Image struct {
	Name     string
	Data     []byte
	MimeType string
}

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
	featureRecursive bool = false
	featureExtract   bool = false
	featureHeight    int  = 800
	featureWidth     int  = 600
	featureQuality   int  = 25
	flags            []string
	queue            []QueuedItem
)

//go:embed templates/*
var templateFS embed.FS

func main() {
	t := time.Now()

	// Parse Arguments
	for i := 1; i < len(os.Args); i++ {
		segments := strings.SplitN(os.Args[i], "=", 2)
		if len(segments) == 2 {
			n := segments[0]
			s := segments[1]
			switch {
			case strings.EqualFold(n, "--height"):
				v := parseInteger(n, s, 128, math.MaxInt)
				log.Printf("Flag: Height %d\n", v)
				featureHeight = v

			case strings.EqualFold(n, "--width"):
				v := parseInteger(n, s, 128, math.MaxInt)
				log.Printf("Flag: Width %d\n", v)
				featureWidth = v

			case strings.EqualFold(n, "--quality"):
				v := parseInteger(n, s, 0, 100)
				log.Printf("Flag: Quality %d\n", v)
				featureQuality = v

			default:
				log.Printf("%s: Unknown Argument", n)
				os.Exit(1)
			}

		} else {
			n := segments[0]
			if strings.EqualFold(n, "--recursive") {
				log.Println("Flag: Scanning Recursively")
				featureRecursive = true
				continue
			}
			if strings.EqualFold(n, "--extract") {
				log.Println("Flag: Extracting Images")
				featureExtract = true
				continue
			}
			flags = append(flags, segments[0])
		}
	}
	if len(flags) < 1 {
		fmt.Println("mangapub")
		fmt.Println("	 --extract			  - Extract Images to Directory")
		fmt.Println("    --recursive          - Scan Directories Recursively")
		fmt.Println("    --height=<value>     - Image Height (Default: 800)")
		fmt.Println("    --width=<value>      - Image Width (Default: 600)")
		fmt.Println("    --quality=<value>    - JPEG Quality (Default: 25, Range: 0-100)")
		os.Exit(0)
	}

	// Process Archives
	scan([]string{})
	for _, info := range queue {

		// Generate Paths
		directory := path.Join(info.Nest...)
		srcPath := path.Join(directory, info.Filename)
		dstPath := path.Join(OUTPUT_DIR, directory, info.Basename)
		if err := os.MkdirAll(path.Join(OUTPUT_DIR, directory), OUTPUT_FLAG); err != nil {
			log.Fatalln("Cannot create output directory:", err)
		}
		log.Printf("Converting: %s\n", srcPath)

		// Convert Archive
		contents, err := ParseCBZ(srcPath)
		if err != nil {
			log.Printf("Failed to parse CBZ '%s': %s\n", srcPath, err)
			continue
		}
		if featureExtract {
			if err := CreateDirectory(contents, dstPath); err != nil {
				log.Printf("Failed to create DIR '%s': %s\n", dstPath, err)
				continue
			}
		} else {
			if err := CreateEPUB(contents, dstPath); err != nil {
				log.Printf("Failed to create EPUB '%s': %s\n", dstPath, err)
				continue
			}
		}

	}

	// Processing Complete
	fmt.Printf("\n")
	log.Printf("Processing Completed in %s\n", time.Since(t))
}

// Parse Integer for CLI Arguments
func parseInteger(n string, s string, min int, max int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		fmt.Printf("%s: Not A Number\n", n)
		os.Exit(1)
	}
	if v < min {
		fmt.Printf("%s: Value cannot be less than %d\n", n, min)
		os.Exit(1)
	}
	if v > max {
		fmt.Printf("%s: Value cannot be more than %d\n", n, max)
		os.Exit(1)
	}
	return v
}

// Scan directory and append eligible items to queue
func scan(nesting []string) {
	if len(nesting) == 1 && strings.EqualFold(nesting[0], OUTPUT_DIR) {
		return
	}

	// Read Entries in Directory
	directory := path.Join(nesting...)
	if directory == "" {
		directory = path.Clean(flags[0])
		nesting = []string{flags[0]}
	}
	dirEntries, err := os.ReadDir(directory)
	if err != nil {
		log.Fatalf("Error reading directory '%s': %s\n", directory, err)
	}

	for _, entry := range dirEntries {
		fileName := entry.Name()

		// Scan Subdirectory
		if entry.IsDir() {
			if featureRecursive {
				scan(append(nesting, fileName))
			}
			continue
		}

		// Add Matching File Extensions to Queue
		fileExt := path.Ext(fileName)
		if !strings.EqualFold(fileExt, ".cbz") {
			continue
		}
		queue = append(queue, QueuedItem{
			Filename: fileName,
			Basename: strings.TrimSuffix(fileName, fileExt),
			Nest:     nesting,
		})
	}
}

func GenerateUUID() string {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		// Fallback to a timestamp-based ID if random generation fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}

	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

func ParseCBZ(filename string) (*File, error) {

	// CBZ files are really just zip archives
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open CBZ file: %w", err)
	}
	defer reader.Close()

	cbzFile := &File{
		Name:   filename,
		Images: []Image{},
	}

	// Multithreaded image processing
	var wc = make(chan int, len(reader.File))
	var wg sync.WaitGroup
	var wm sync.Mutex
	for c := 0; c < runtime.NumCPU(); c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range wc {

				// Ignore Directories
				file := reader.File[i]
				if file.FileInfo().IsDir() {
					continue
				}

				// Read file contents inside archive
				rc, err := file.Open()
				if err != nil {
					log.Printf("failed to open file in CBZ: %s\n", err)
					continue
				}
				d, _ := io.ReadAll(rc)
				rc.Close()

				// Decode Image with the appropriate decoder based on it's starting bytes
				// https://en.wikipedia.org/wiki/Magic_number_(programming)#Magic_numbers_in_files)
				var decoderImage image.Image
				var decoderError error
				switch {
				case len(d) > 3 && // JPEG
					d[0] == 0xFF && d[1] == 0xD8 && d[2] == 0xFF:
					decoderImage, decoderError = jpeg.Decode(bytes.NewReader(d))

				case len(d) > 8 && // PNG
					d[0] == 0x89 && d[1] == 0x50 && d[2] == 0x4E && d[3] == 0x47 &&
					d[4] == 0x0D && d[5] == 0x0A && d[6] == 0x1A && d[7] == 0x0A:
					decoderImage, decoderError = png.Decode(bytes.NewReader(d))

				case len(d) > 4 && // GIF
					d[0] == 0x47 && d[1] == 0x49 && d[2] == 0x46 && d[3] == 0x38:
					decoderImage, decoderError = gif.Decode(bytes.NewReader(d))

				case len(d) > 12 && // WEBP
					d[0] == 0x52 && d[1] == 0x49 && d[2] == 0x46 && d[3] == 0x46 &&
					d[8] == 0x57 && d[9] == 0x45 && d[10] == 0x42 && d[11] == 0x50:
					decoderImage, decoderError = webp.Decode(bytes.NewReader(d))

				default: // unsupported content type
					continue
				}
				if decoderError != nil {
					log.Printf("malformed image: %s\n", err)
					continue
				}

				// Calculate Scaled Height and Width
				bounds := decoderImage.Bounds()
				targetW, targetH := featureWidth, featureHeight
				iw, ih := bounds.Dx(), bounds.Dy()
				ratio := math.Min(float64(targetW)/float64(iw), float64(targetH)/float64(ih))
				sw, sh := int(float64(iw)*ratio), int(float64(ih)*ratio)
				canvas := image.NewRGBA(image.Rect(0, 0, targetW, targetH))

				// Resize Image (White Background)
				for x := 0; x < targetW; x++ {
					for y := 0; y < targetH; y++ {
						canvas.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
					}
				}
				offsetX := (targetW - sw) / 2
				offsetY := (targetH - sh) / 2
				draw.CatmullRom.Scale(canvas, image.Rect(offsetX, offsetY, offsetX+sw, offsetY+sh),
					decoderImage, bounds, draw.Over, nil)

				// Encode Resized Image into JPEG
				enc := bytes.Buffer{}
				if err := jpeg.Encode(&enc, canvas, &jpeg.Options{Quality: featureQuality}); err != nil {
					log.Printf("encoding error: %s\n", err)
					continue
				}

				// Append Image to List
				wm.Lock()
				cbzFile.Images = append(cbzFile.Images, Image{
					Name:     strings.TrimSuffix(path.Base(file.Name), path.Ext(file.Name)) + ".jpeg",
					Data:     enc.Bytes(),
					MimeType: "image/jpeg",
				})
				wm.Unlock()
			}
		}()
	}

	// Wait for processing to complete
	for i := 0; i < len(reader.File); i++ {
		wc <- i
	}
	close(wc)
	wg.Wait()

	// Sort Images by Name
	sort.Slice(cbzFile.Images, func(i, j int) bool {
		return cbzFile.Images[i].Name < cbzFile.Images[j].Name
	})
	return cbzFile, nil
}

func CreateEPUB(input *File, filename string) error {

	// EPUB files are really just zip archives
	writer, err := os.Create(filename + ".epub")
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer writer.Close()

	archive := zip.NewWriter(writer)
	defer archive.Close()

	{
		// Write Mime Header
		mimetype, err := archive.CreateHeader(&zip.FileHeader{
			Name:   "mimetype",
			Method: zip.Store,
		})
		if err != nil {
			return fmt.Errorf("failed to create mimetype file: %w", err)
		}
		if _, err = mimetype.Write([]byte("application/epub+zip")); err != nil {
			return fmt.Errorf("failed to write mimetype file: %w", err)
		}
	}

	type Item struct {
		ID   int
		Base string
		Type string
	}
	var (
		ContentTitle  = strings.TrimSuffix(path.Base(input.Name), path.Ext(input.Name))
		ContentDate   = time.Now().Format("2006-01-02")
		ContentUUID   = GenerateUUID()
		ContentImages = make([]Item, 0, len(input.Images))
	)
	for i, image := range input.Images {

		// Create Metadata Entry
		pathBase := fmt.Sprintf("page%03d", i+1)
		pathItem := Item{
			ID:   i + 1,
			Base: pathBase,
			Type: image.MimeType,
		}

		// Add HTML to Archive
		{
			pathOutput := fmt.Sprint("OEBPS/pages/", pathBase, ".xhtml")
			pathTemplate := "templates/page.xml"
			tmpl, err := template.ParseFS(templateFS, pathTemplate)
			if err != nil {
				return fmt.Errorf("cannot open template file '%s': %s", pathTemplate, err)
			}
			output, err := archive.Create(pathOutput)
			if err != nil {
				return fmt.Errorf("cannot create archive file '%s': %s", pathOutput, err)
			}
			if err := tmpl.Execute(output, pathItem); err != nil {
				return fmt.Errorf("cannot execute template file '%s': %s", pathTemplate, err)
			}
		}

		// Add Image to Archive
		{
			pathOutput := fmt.Sprint("OEBPS/images/", pathBase, ".jpeg")
			output, err := archive.Create(pathOutput)
			if err != nil {
				return fmt.Errorf("cannot create archive file '%s': %s", pathOutput, err)
			}
			if _, err = output.Write(image.Data); err != nil {
				return fmt.Errorf("cannot write archive file '%s': %s", pathOutput, err)
			}
		}

		ContentImages = append(ContentImages, pathItem)
	}

	{
		// Generate Metadata with Templates
		literals := map[string]any{
			"ContentTitle":  ContentTitle,
			"ContentDate":   ContentDate,
			"ContentUUID":   ContentUUID,
			"ContentImages": ContentImages,
		}
		for _, meta := range [][]string{
			{"OEBPS/content.opf", "templates/content.opf"},
			{"OEBPS/toc.ncx", "templates/toc.ncx"},
			{"META-INF/container.xml", "templates/container.xml"},
		} {
			pathOutput := meta[0]
			pathTemplate := meta[1]
			tmpl, err := template.ParseFS(templateFS, pathTemplate)
			if err != nil {
				return fmt.Errorf("cannot open template file '%s': %s", pathTemplate, err)
			}
			output, err := archive.Create(pathOutput)
			if err != nil {
				return fmt.Errorf("cannot create archive file '%s': %s", pathOutput, err)
			}
			if err := tmpl.Execute(output, literals); err != nil {
				return fmt.Errorf("cannot execute template file '%s': %s", pathTemplate, err)
			}
		}
	}

	return nil
}

func CreateDirectory(input *File, filename string) error {

	// Create Output Directory
	if err := os.MkdirAll(filename, OUTPUT_FLAG); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	// Write Images
	for i, image := range input.Images {
		imageName := fmt.Sprintf("page%03d.jpeg", i+1)
		imagePath := path.Join(filename, imageName)
		if err := os.WriteFile(imagePath, image.Data, OUTPUT_FLAG); err != nil {
			return fmt.Errorf("failed to write image: %w", err)
		}
	}

	return nil
}
