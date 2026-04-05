package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"os"
	"os/exec"

	"golang.org/x/image/draw"
	"image/png"
)

func main() {
	f, err := os.Open("data_debug/data_debug/captcha_original.png")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer f.Close()
	imgBytes, _ := io.ReadAll(f)
	
	src, _, _ := image.Decode(bytes.NewReader(imgBytes))
	b := src.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, src, b.Min, draw.Src)

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

	eroded := erodeWhite(thresh)
	dilated1 := dilateWhite(eroded)
	dilated2 := dilateWhite(dilated1)

	finalImg := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := dilated2.GrayAt(x, y).Y
			finalImg.SetGray(x, y, color.Gray{Y: ^v})
		}
	}

	scaled := image.NewGray(image.Rect(0, 0, b.Dx()*3, b.Dy()*3))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), finalImg, b, draw.Src, nil)

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

	outPath := "data_debug/test_out.png"
	outFile, _ := os.Create(outPath)
	png.Encode(outFile, padded)
	outFile.Close()

	cmd := exec.Command(`C:\Program Files\Tesseract-OCR\tesseract.exe`, outPath, "stdout", "--psm", "8", "-c", "tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	fmt.Printf("OCR Output: '%s', error: %v\n", stdout.String(), err)
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
