package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"path"
	"strconv"
	"strings"

	"golang.org/x/image/webp"
)

func main() {

	// ----- Parse Arguments -----
	var optionQuality = 0
	var optionNoise = 25
	var optionGenerations = 5
	var optionFilename string

	flags := make([]string, 0, len(os.Args))
	for i := 1; i < len(os.Args); i++ {
		segments := strings.SplitN(os.Args[i], "=", 2)
		if len(segments) == 2 {
			n := segments[0]
			s := segments[1]
			switch {
			case strings.EqualFold(n, "--generations"):
				v := parseInteger(n, s, 0, math.MaxInt)
				fmt.Printf("Flag: Generation(s) %d\n", v)
				optionGenerations = v

			case strings.EqualFold(n, "--quality"):
				v := parseInteger(n, s, 0, 100)
				fmt.Printf("Flag: Quality %d\n", v)
				optionQuality = v

			case strings.EqualFold(n, "--noise"):
				v := parseInteger(n, s, 0, 100)
				fmt.Printf("Flag: Noise Level %d\n", v)
				optionQuality = v

			default:
				fmt.Printf("%s: Unknown Argument", n)
				os.Exit(1)
			}

		} else {
			flags = append(flags, segments[0])
		}
	}
	if len(flags) < 1 {
		fmt.Println("crunchy")
		fmt.Println("	 --noise=<value>       - Noise Level  (Default: 25, Range: 0-100)")
		fmt.Println("	 --quality=<value>	   - JPEG Quality (Default: 0,  Range: 0-100)")
		fmt.Println("    --generations=<count> - Iterations   (Default: 5)")
		fmt.Println("    <Filename>            - Input Filename")
		os.Exit(0)
	}
	optionFilename = flags[0]
	noiseInteger := int(float32(optionNoise)*2.56) + 1
	noiseHalved := noiseInteger / 2

	// ----- Decode Image Contents -----
	content := bytes.Buffer{}
	f, err := os.Open(optionFilename)
	if err != nil {
		fmt.Printf("Failed to open file: %s\n", err.Error())
		os.Exit(1)
	}
	if _, err := io.Copy(&content, f); err != nil {
		fmt.Printf("Failed to read file: %s\n", err.Error())
		os.Exit(1)
	}

	img, err := decodeImage(content.Bytes())
	if err != nil {
		fmt.Printf("Decoding Error: %s\n", err.Error())
		os.Exit(1)
	}
	bounds := img.Bounds()
	ycc := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio444)
	rgb := image.NewRGBA(bounds)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			rgb.Set(x, y, img.At(x, y)) // copy generic img to rgba
		}
	}

	// ----- Apply Generation Loss -----
	for i := 0; i < optionGenerations; i++ {
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				// Rounding Error via Colorspace Conversion
				r, g, b, _ := rgb.At(x, y).RGBA()
				cy, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(b>>8))

				// Random Noise
				noise := rand.Intn(noiseInteger) - noiseHalved
				cb = uint8(int(cb) + noise)
				cr = uint8(int(cr) + noise)

				// Apply Changes
				ycc.Y[ycc.YOffset(x, y)] = cy
				ycc.Cb[ycc.COffset(x, y)] = cb
				ycc.Cr[ycc.COffset(x, y)] = cr
			}
		}
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				rgb.Set(x, y, ycc.At(x, y))
			}
		}
	}

	// ----- Write Output -----
	content.Reset()
	if err := jpeg.Encode(&content, rgb, &jpeg.Options{Quality: optionQuality}); err != nil {
		fmt.Printf("Encoding Error: %s\n", err.Error())
		os.Exit(1)
	}
	cleanname := path.Base(optionFilename)
	emptyname := strings.TrimSuffix(cleanname, path.Ext(cleanname))
	finalname := fmt.Sprintf("%s_n%d_g%d_q%d.jpeg", emptyname, optionNoise, optionGenerations, optionQuality)
	if err := os.WriteFile(finalname, content.Bytes(), 0660); err != nil {
		fmt.Printf("Failed to write file '%s': %s\n", finalname, err.Error())
		os.Exit(1)
	}
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

// Decode Image with the appropriate decoder based on it's starting bytes
// https://en.wikipedia.org/wiki/Magic_number_(programming)#Magic_numbers_in_files)
func decodeImage(d []byte) (image.Image, error) {
	var (
		decoderImage image.Image
		decoderError error
	)
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

	default:
		return decoderImage, errors.New("unsupported file type")
	}
	if decoderError != nil {
		return nil, decoderError
	}

	return decoderImage, nil
}
