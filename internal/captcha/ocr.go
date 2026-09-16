package captcha

import (
	"bufio"
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
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/image/draw"
)

const candidateAgreementBonus = 30.0

var captchaThresholds = []uint8{105, 115, 125, 135}

type ocrCandidate struct {
	text       string
	confidence float64
	variant    string
}

type imageVariant struct {
	name string
	data []byte
}

// Solve runs an OCR ensemble over the same CAPTCHA and returns the strongest
// four-character reading. The thresholds are based on Atlas OIS's live CAPTCHA
// palette: glyphs are darker than the interference curves and speckle noise.
func Solve(imgBytes []byte) (string, error) {
	src, format, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", err
	}
	log.Printf("[ocr] input: format=%s, size=%dx%d", format, src.Bounds().Dx(), src.Bounds().Dy())

	debugDir := filepath.Join(".", "data_debug")
	_ = os.MkdirAll(debugDir, 0755)
	_ = os.WriteFile(filepath.Join(debugDir, "captcha_original.png"), imgBytes, 0644)

	tesseractPath, err := findTesseract()
	if err != nil {
		return "", err
	}

	variants := []imageVariant{{name: "original", data: imgBytes}}
	for _, threshold := range captchaThresholds {
		processed, err := buildVariant(src, threshold)
		if err != nil {
			return "", err
		}
		variants = append(variants, imageVariant{
			name: fmt.Sprintf("threshold_%d", threshold),
			data: processed,
		})
	}

	tempDir, err := os.MkdirTemp("", "ois-captcha-ocr-")
	if err != nil {
		return "", fmt.Errorf("OCR geçici dizini: %w", err)
	}
	defer os.RemoveAll(tempDir)

	var candidates []ocrCandidate
	variantData := make(map[string][]byte, len(variants))
	var lastRunErr error
	successfulRuns := 0
	for _, variant := range variants {
		variantData[variant.name] = variant.data
		path := filepath.Join(tempDir, variant.name+".png")
		if err := os.WriteFile(path, variant.data, 0600); err != nil {
			return "", fmt.Errorf("OCR görseli yaz: %w", err)
		}

		for _, psm := range []int{8, 10} {
			candidate, err := runTesseract(tesseractPath, path, psm)
			if err != nil {
				lastRunErr = err
				log.Printf("[ocr] variant=%s psm=%d hata=%v", variant.name, psm, err)
				continue
			}
			successfulRuns++
			candidate.variant = variant.name
			candidates = append(candidates, candidate)
			log.Printf("[ocr] variant=%s psm=%d text=%q confidence=%.1f", variant.name, psm, candidate.text, candidate.confidence)
		}
	}

	if successfulRuns == 0 {
		return "", fmt.Errorf("tesseract hatası: %w", lastRunErr)
	}

	result := selectCandidate(candidates)
	if result == "" {
		log.Printf("[ocr] dört karakterli aday bulunamadı")
		return "", nil
	}

	bestConfidence := -1.0
	for _, candidate := range candidates {
		if candidate.text == result && candidate.confidence > bestConfidence {
			bestConfidence = candidate.confidence
			_ = os.WriteFile(filepath.Join(debugDir, "captcha_processed.png"), variantData[candidate.variant], 0644)
		}
	}

	log.Printf("[ocr] selected=%q", result)
	return result, nil
}

func findTesseract() (string, error) {
	paths := []string{
		"tesseract",
		`C:\Program Files\Tesseract-OCR\tesseract.exe`,
		`C:\Program Files (x86)\Tesseract-OCR\tesseract.exe`,
	}
	for _, path := range paths {
		if found, err := exec.LookPath(path); err == nil {
			return found, nil
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("tesseract bulunamadı")
}

func buildVariant(src image.Image, threshold uint8) ([]byte, error) {
	bounds := src.Bounds()
	marginX := (bounds.Dx()*20 + 107) / 215
	marginY := (bounds.Dy()*5 + 40) / 80
	crop := image.Rect(
		bounds.Min.X+marginX,
		bounds.Min.Y+marginY,
		bounds.Max.X-marginX,
		bounds.Max.Y-marginY,
	)
	if crop.Dx() <= 0 || crop.Dy() <= 0 {
		crop = bounds
	}

	gray := image.NewGray(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(gray, gray.Bounds(), src, crop.Min, draw.Src)
	binary := image.NewGray(gray.Bounds())
	for y := 0; y < gray.Bounds().Dy(); y++ {
		for x := 0; x < gray.Bounds().Dx(); x++ {
			value := uint8(255)
			if gray.GrayAt(x, y).Y < threshold {
				value = 0
			}
			binary.SetGray(x, y, color.Gray{Y: value})
		}
	}

	scaled := image.NewGray(image.Rect(0, 0, binary.Bounds().Dx()*4, binary.Bounds().Dy()*4))
	draw.NearestNeighbor.Scale(scaled, scaled.Bounds(), binary, binary.Bounds(), draw.Src, nil)

	const padding = 20
	padded := image.NewGray(image.Rect(0, 0, scaled.Bounds().Dx()+padding*2, scaled.Bounds().Dy()+padding*2))
	for i := range padded.Pix {
		padded.Pix[i] = 255
	}
	draw.Draw(padded, image.Rect(padding, padding, padding+scaled.Bounds().Dx(), padding+scaled.Bounds().Dy()), scaled, scaled.Bounds().Min, draw.Src)

	var output bytes.Buffer
	if err := png.Encode(&output, padded); err != nil {
		return nil, fmt.Errorf("OCR görselini kodla: %w", err)
	}
	return output.Bytes(), nil
}

func runTesseract(tesseractPath, imagePath string, psm int) (ocrCandidate, error) {
	cmd := exec.Command(
		tesseractPath,
		imagePath,
		"stdout",
		"--psm", strconv.Itoa(psm),
		"-c", "tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"tsv",
	)
	output, err := cmd.Output()
	if err != nil {
		return ocrCandidate{}, err
	}
	text, confidence := parseTesseractTSV(output)
	return ocrCandidate{text: text, confidence: confidence}, nil
}

func parseTesseractTSV(output []byte) (string, float64) {
	var text strings.Builder
	confidenceTotal := 0.0
	wordCount := 0
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 12)
		if len(fields) != 12 || fields[0] != "5" || fields[11] == "" {
			continue
		}
		text.WriteString(fields[11])
		confidence, err := strconv.ParseFloat(fields[10], 64)
		if err == nil && confidence >= 0 {
			confidenceTotal += confidence
			wordCount++
		}
	}

	confidence := 0.0
	if wordCount > 0 {
		confidence = confidenceTotal / float64(wordCount)
	}
	return strings.ToLower(sanitize(text.String())), confidence
}

func selectCandidate(candidates []ocrCandidate) string {
	type candidateStats struct {
		votes         int
		maxConfidence float64
	}

	stats := make(map[string]candidateStats)
	order := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !IsValid(candidate.text) {
			continue
		}
		current, exists := stats[candidate.text]
		if !exists {
			order = append(order, candidate.text)
			current.maxConfidence = candidate.confidence
		}
		current.votes++
		if candidate.confidence > current.maxConfidence {
			current.maxConfidence = candidate.confidence
		}
		stats[candidate.text] = current
	}

	bestText := ""
	bestScore := -1.0
	bestVotes := -1
	for _, text := range order {
		candidate := stats[text]
		score := candidate.maxConfidence + float64(candidate.votes-1)*candidateAgreementBonus
		if score > bestScore || (score == bestScore && candidate.votes > bestVotes) {
			bestText = text
			bestScore = score
			bestVotes = candidate.votes
		}
	}
	return bestText
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

// IsValid reports whether OCR output matches Atlas OIS's four-character CAPTCHA.
func IsValid(text string) bool {
	if len(text) != 4 {
		return false
	}
	for _, char := range text {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
			return false
		}
	}
	return true
}
