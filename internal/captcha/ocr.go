package captcha

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/image/draw"
)

// Solve ham CAPTCHA byte'larını alır, metin döner.
func Solve(imgBytes []byte) (string, error) {
	_ = os.MkdirAll(filepath.Join(".", "data_debug"), 0755)

	src, format, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", err
	}
	log.Printf("[ocr] input: format=%s, size=%dx%d", format, src.Bounds().Dx(), src.Bounds().Dy())

	// Debug: orijinal CAPTCHA'yı kaydet
	_ = os.WriteFile(filepath.Join(".", "data_debug", "captcha_original.png"), imgBytes, 0644)

	b := src.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, src, b.Min, draw.Src)

	// 1. Threshold (Text=White, BG=Black)
	thresh := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if gray.GrayAt(x, y).Y <= 160 {
				thresh.SetGray(x, y, color.Gray{Y: 255})
			} else {
				thresh.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}

	// 2. Morphological OPEN + Dilate
	eroded := erodeWhite(thresh)
	dilated1 := dilateWhite(eroded)
	dilated2 := dilateWhite(dilated1)

	// 3. Invert back (Text=Black, BG=White)
	finalImg := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := dilated2.GrayAt(x, y).Y
			finalImg.SetGray(x, y, color.Gray{Y: ^v})
		}
	}

	// 4. Scale 3x
	scaled := image.NewGray(image.Rect(0, 0, b.Dx()*3, b.Dy()*3))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), finalImg, b, draw.Src, nil)

	// 5. Padding
	pad := 20
	padded := image.NewGray(image.Rect(0, 0, scaled.Bounds().Dx()+pad*2, scaled.Bounds().Dy()+pad*2))
	for y := 0; y < padded.Bounds().Dy(); y++ {
		for x := 0; x < padded.Bounds().Dx(); x++ {
			padded.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	for y := scaled.Bounds().Min.Y; y < scaled.Bounds().Max.Y; y++ {
		for x := scaled.Bounds().Min.X; x < scaled.Bounds().Max.X; x++ {
			padded.SetGray(x+pad, y+pad, scaled.GrayAt(x, y))
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, padded)

	// Debug: işlenmiş CAPTCHA'yı kaydet
	processedPath := filepath.Join(".", "data_debug", "captcha_processed.png")
	_ = os.WriteFile(processedPath, buf.Bytes(), 0644)

	tesseractPaths := []string{
		"tesseract",
		`C:\Program Files\Tesseract-OCR\tesseract.exe`,
		`C:\Program Files (x86)\Tesseract-OCR\tesseract.exe`,
	}
	var foundPath string
	for _, p := range tesseractPaths {
		if _, err := exec.LookPath(p); err == nil {
			foundPath = p
			break
		}
		if _, err := os.Stat(p); err == nil {
			foundPath = p
			break
		}
	}
	if foundPath == "" {
		return "", fmt.Errorf("tesseract bulunamadı")
	}

	cmd := exec.Command(foundPath, processedPath, "stdout", "--psm", "8", "-c", "tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tesseract hatası: %v", err)
	}

	text := stdout.String()
	sanitized := sanitize(text)
	result := strings.ToLower(sanitized) // CAPTCHA genelde büyük/küçük harf duyarsız
	log.Printf("[ocr] raw=%q → sanitized=%q → result=%q", text, sanitized, result)
	return result, nil
}

func erodeWhite(img *image.Gray) *image.Gray {
	b := img.Bounds()
	out := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if y == b.Max.Y-1 || x == b.Max.X-1 {
				out.SetGray(x, y, color.Gray{Y: 0})
				continue
			}
			if img.GrayAt(x, y).Y == 255 && img.GrayAt(x+1, y).Y == 255 && img.GrayAt(x, y+1).Y == 255 && img.GrayAt(x+1, y+1).Y == 255 {
				out.SetGray(x, y, color.Gray{Y: 255})
			} else {
				out.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return out
}

func dilateWhite(img *image.Gray) *image.Gray {
	b := img.Bounds()
	out := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if y == b.Max.Y-1 || x == b.Max.X-1 {
				out.SetGray(x, y, color.Gray{Y: 0})
				continue
			}
			if img.GrayAt(x, y).Y == 255 || img.GrayAt(x+1, y).Y == 255 || img.GrayAt(x, y+1).Y == 255 || img.GrayAt(x+1, y+1).Y == 255 {
				out.SetGray(x, y, color.Gray{Y: 255})
			} else {
				out.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return out
}

// sanitize OCR çıktısındaki boşluk ve özel karakterleri temizler.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
