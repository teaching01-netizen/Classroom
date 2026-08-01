// qrdecode decodes the payload of a QR code from an image file, an http(s)
// URL, or a base64 data URI (the shape of the qr_url returned by the API).
//
// Usage:
//
//	go run . <path|http(s)-url|data:image/...>
//
//	# self-test: generate a QR and decode it back
//	go run . --self-test
//
// Prints the decoded payload to stdout. Exit code 0 = decoded, 1 = none.
// Pure Go (github.com/makiuchi-d/gozxing) — no cgo, no system libraries.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: qrdecode <path|http(s)-url|data:image/...> | --self-test")
		os.Exit(2)
	}
	if os.Args[1] == "--self-test" {
		selfTest()
		return
	}
	payload, err := decode(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "qrdecode: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(payload)
}

func decode(input string) (string, error) {
	var r io.Reader
	switch {
	case strings.HasPrefix(input, "data:"):
		// data:image/png;base64,<payload>  (Warwick emits a space after the comma)
		comma := strings.IndexByte(input, ',')
		if comma < 0 {
			return "", fmt.Errorf("malformed data URI")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input[comma+1:]))
		if err != nil {
			return "", fmt.Errorf("base64 decode: %w", err)
		}
		r = bytes.NewReader(raw)
	case strings.HasPrefix(input, "http://"), strings.HasPrefix(input, "https://"):
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(input)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("GET %s: status %d", input, resp.StatusCode)
		}
		r = resp.Body
	default:
		f, err := os.Open(input)
		if err != nil {
			return "", err
		}
		defer f.Close()
		r = f
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	return decodeFromImage(img)
}

func decodeFromImage(img image.Image) (string, error) {
	reader := qrcode.NewQRCodeReader()
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("prepare bitmap: %w", err)
	}
	result, err := reader.Decode(bitmap, nil)
	if err != nil {
		return "", fmt.Errorf("no QR code found: %w", err)
	}
	return result.GetText(), nil
}

// selfTest generates a QR with gozxing's writer and decodes it back, proving
// the decode path works without needing an external QR image.
func selfTest() {
	writer := qrcode.NewQRCodeWriter()
	matrix, err := writer.Encode("https://warwick.humantix.cloud/Student/Checkin?attendanceId=18898", gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "self-test encode failed: %v\n", err)
		os.Exit(1)
	}
	// gozxing's matrix is luminance bytes at width x height.
	bw := matrix.GetWidth()
	bh := matrix.GetHeight()
	pix := make([]uint8, bw*bh)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			if matrix.Get(x, y) {
				pix[y*bw+x] = 0
			} else {
				pix[y*bw+x] = 255
			}
		}
	}
	img := &grayImage{pix: pix, w: bw, h: bh}
	got, err := decodeFromImage(img)
	if err != nil {
		fmt.Fprintf(os.Stderr, "self-test decode failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("self-test OK: decoded %q\n", got)
}

type grayImage struct {
	pix []uint8
	w   int
	h   int
}

func (g *grayImage) ColorModel() color.Model { return color.GrayModel }
func (g *grayImage) Bounds() image.Rectangle { return image.Rect(0, 0, g.w, g.h) }
func (g *grayImage) At(x, y int) color.Color { return color.Gray{Y: g.pix[y*g.w+x]} }
