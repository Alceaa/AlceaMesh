package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const outputDir = "assets"

var (
	glyphColor = color.RGBA{R: 0xe2, G: 0xb0, B: 0x53, A: 0xff}
	topColor   = color.RGBA{R: 0x1d, G: 0x19, B: 0x15, A: 0xff}
	lowColor   = color.RGBA{R: 0x0b, G: 0x0a, B: 0x09, A: 0xff}
	edgeColor  = color.RGBA{R: 0x3a, G: 0x31, B: 0x24, A: 0xff}
)

type glyph struct {
	rings      [][3]float64
	segments   [][5]float64
	strokeHalf float64
}

func main() {
	master := render(256, 8)
	if err := savePNG(filepath.Join(outputDir, "icon.png"), master); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sizes := []int{16, 32, 48, 64, 128, 256}
	frames := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		data, err := encodePNG(render(size, supersample(size)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		frames = append(frames, data)
	}
	if err := writeICO(filepath.Join(outputDir, "icon.ico"), sizes, frames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("written: %s/icon.png, %s/icon.ico (%d sizes)\n", outputDir, outputDir, len(sizes))
}

func supersample(size int) int {
	switch {
	case size <= 32:
		return 16
	case size <= 64:
		return 12
	default:
		return 8
	}
}

func render(size, factor int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	shape := glyph{
		rings: [][3]float64{
			{0.25, 0.26667, 0.1},
			{0.75, 0.26667, 0.1},
			{0.5, 0.74167, 0.1},
		},
		segments: [][5]float64{
			{0.34167, 0.30833, 0.42917, 0.65},
			{0.65833, 0.30833, 0.57083, 0.65},
			{0.35, 0.26667, 0.65, 0.26667},
		},
		strokeHalf: 0.0375,
	}

	samples := factor * factor
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var red, green, blue, alpha float64
			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/float64(factor)) / float64(size)
					py := (float64(y) + (float64(sy)+0.5)/float64(factor)) / float64(size)

					coverage := panelCoverage(px, py)
					if coverage <= 0 {
						continue
					}

					background := edgeColor
					if insidePanel(px, py) {
						background = mix(topColor, lowColor, py)
					}
					if shape.inside(px, py) {
						background = glyphColor
					}
					red += float64(background.R) * coverage
					green += float64(background.G) * coverage
					blue += float64(background.B) * coverage
					alpha += coverage
				}
			}
			img.Set(x, y, color.RGBA{
				R: clamp(red / float64(samples)),
				G: clamp(green / float64(samples)),
				B: clamp(blue / float64(samples)),
				A: clamp(alpha / float64(samples) * 255),
			})
		}
	}
	return img
}

func insidePanel(x, y float64) bool {
	return roundedRect(x-0.5, y-0.5, 0.44, 0.44, 0.19) <= -0.012
}

func panelCoverage(x, y float64) float64 {
	distance := roundedRect(x-0.5, y-0.5, 0.44, 0.44, 0.19)
	switch {
	case distance <= -0.012:
		return 1
	case distance >= 0.012:
		return 0
	default:
		return (0.012 - distance) / 0.024
	}
}

func roundedRect(x, y, halfWidth, halfHeight, radius float64) float64 {
	qx := math.Abs(x) - (halfWidth - radius)
	qy := math.Abs(y) - (halfHeight - radius)
	return math.Min(math.Max(qx, qy), 0) + math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) - radius
}

func (g glyph) inside(x, y float64) bool {
	for _, ring := range g.rings {
		distance := math.Hypot(x-ring[0], y-ring[1])
		if math.Abs(distance-ring[2]) <= g.strokeHalf {
			return true
		}
	}
	for _, segment := range g.segments {
		if distanceToSegment(x, y, segment) <= g.strokeHalf {
			return true
		}
	}
	return false
}

func distanceToSegment(x, y float64, segment [5]float64) float64 {
	ax, ay, bx, by := segment[0], segment[1], segment[2], segment[3]
	dx, dy := bx-ax, by-ay
	length := dx*dx + dy*dy
	position := 0.0
	if length > 0 {
		position = ((x-ax)*dx + (y-ay)*dy) / length
	}
	position = math.Max(0, math.Min(1, position))
	return math.Hypot(x-(ax+position*dx), y-(ay+position*dy))
}

func mix(from, to color.RGBA, position float64) color.RGBA {
	position = math.Max(0, math.Min(1, position))
	return color.RGBA{
		R: uint8(float64(from.R) + (float64(to.R)-float64(from.R))*position),
		G: uint8(float64(from.G) + (float64(to.G)-float64(from.G))*position),
		B: uint8(float64(from.B) + (float64(to.B)-float64(from.B))*position),
		A: 0xff,
	}
}

func clamp(value float64) uint8 {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(value + 0.5)
}

func encodePNG(img image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func savePNG(path string, img image.Image) error {
	data, err := encodePNG(img)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeICO(path string, sizes []int, frames [][]byte) error {
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(0)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(len(frames))); err != nil {
		return err
	}

	offset := 6 + 16*len(frames)
	for index, frame := range frames {
		dimension := byte(sizes[index])
		if sizes[index] >= 256 {
			dimension = 0
		}
		buffer.WriteByte(dimension)
		buffer.WriteByte(dimension)
		buffer.WriteByte(0)
		buffer.WriteByte(0)
		if err := binary.Write(&buffer, binary.LittleEndian, uint16(1)); err != nil {
			return err
		}
		if err := binary.Write(&buffer, binary.LittleEndian, uint16(32)); err != nil {
			return err
		}
		if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(frame))); err != nil {
			return err
		}
		if err := binary.Write(&buffer, binary.LittleEndian, uint32(offset)); err != nil {
			return err
		}
		offset += len(frame)
	}
	for _, frame := range frames {
		buffer.Write(frame)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}
