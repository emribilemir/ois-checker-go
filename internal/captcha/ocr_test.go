package captcha

import (
	"errors"
	"testing"
)

func TestIsValidRejectsOCRTextWithWrongLength(t *testing.T) {
	if IsValid("ld4hvjiz") {
		t.Fatal("expected an eight-character OCR result to be rejected")
	}
	if !IsValid("3y3b") {
		t.Fatal("expected a four-character CAPTCHA result to be accepted")
	}
}

func TestSelectCandidateBalancesConfidenceWithAgreement(t *testing.T) {
	candidates := []ocrCandidate{
		{text: "bazs", confidence: 0},
		{text: "b4zs", confidence: 0},
		{text: "e4zs", confidence: 80.107},
		{text: "b4zs", confidence: 0},
		{text: "e4zs", confidence: 65.919},
		{text: "b4zs", confidence: 0},
		{text: "e4275", confidence: 0},
		{text: "b4zs", confidence: 0},
		{text: "e4275", confidence: 18.628},
	}

	if got := selectCandidate(candidates); got != "e4zs" {
		t.Fatalf("expected the server-verified live CAPTCHA reading e4zs, got %q", got)
	}
}

func TestSelectCandidateIgnoresReadingsWithWrongLength(t *testing.T) {
	candidates := []ocrCandidate{
		{text: "fhku", confidence: 0},
		{text: "7fhku", confidence: 99},
		{text: "fhku", confidence: 0},
	}

	if got := selectCandidate(candidates); got != "fhku" {
		t.Fatalf("expected four-character consensus, got %q", got)
	}
}

func TestParseTesseractTSVReturnsSanitizedTextAndMeanConfidence(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t80.0\tE4\n" +
		"5\t1\t1\t1\t1\t2\t10\t0\t10\t10\t60.0\tzS!\n"

	text, confidence := parseTesseractTSV([]byte(tsv))
	if text != "e4zs" {
		t.Fatalf("expected sanitized lowercase text, got %q", text)
	}
	if confidence != 70 {
		t.Fatalf("expected mean confidence 70, got %.3f", confidence)
	}
}

func scripted(results []ocrCandidate) (func(ocrPass) (ocrCandidate, error), []ocrPass) {
	passes := make([]ocrPass, len(results))
	i := 0
	return func(ocrPass) (ocrCandidate, error) {
		r := results[i]
		i++
		if r.text == "ERR" {
			return ocrCandidate{}, errors.New("tesseract çöktü")
		}
		return r, nil
	}, passes
}

func TestEnsembleDoesNotStopEarlyOnConfidentlessMajority(t *testing.T) {
	// Canlı sitede sunucunun kabul ettiği okuma e4zs; b4zs sıfır güvenle 4 oy topluyor.
	live := []ocrCandidate{
		{text: "bazs", confidence: 0}, {text: "b4zs", confidence: 0}, {text: "e4zs", confidence: 80.107},
		{text: "b4zs", confidence: 0}, {text: "e4zs", confidence: 65.919}, {text: "b4zs", confidence: 0},
		{text: "e4275", confidence: 0}, {text: "b4zs", confidence: 0}, {text: "e4275", confidence: 18.628},
	}
	run, passes := scripted(live)
	candidates, runs, _ := runEnsemble(passes, run)
	if runs != len(live) || selectCandidate(candidates) != "e4zs" {
		t.Fatalf("expected the full ensemble to pick e4zs, runs=%d got=%q", runs, selectCandidate(candidates))
	}
}

func TestEnsembleStopsOnceAConfidentReadingIsConfirmed(t *testing.T) {
	clean := []ocrCandidate{
		{text: "k7mp", confidence: 85}, {text: "k7np", confidence: 40}, {text: "k7mp", confidence: 72},
		{text: "k7mp", confidence: 0}, {text: "zzzz", confidence: 99}, {text: "zzzz", confidence: 99},
		{text: "zzzz", confidence: 99}, {text: "zzzz", confidence: 99}, {text: "zzzz", confidence: 99}, {text: "zzzz", confidence: 99},
	}
	run, passes := scripted(clean)
	candidates, runs, _ := runEnsemble(passes, run)
	if runs != 4 || selectCandidate(candidates) != "k7mp" {
		t.Fatalf("expected to stop after 4 readings with k7mp, runs=%d got=%q", runs, selectCandidate(candidates))
	}
}

func TestEnsembleKeepsGoingAfterTesseractErrors(t *testing.T) {
	run, passes := scripted([]ocrCandidate{{text: "ERR"}, {text: "ERR"}, {text: "ab12", confidence: 90}, {text: "ab12", confidence: 90}, {text: "ab12", confidence: 90}, {text: "ERR"}})
	candidates, runs, lastErr := runEnsemble(passes, run)
	if runs != 5 || len(candidates) != 3 || lastErr == nil {
		t.Fatalf("runs=%d candidates=%d lastErr=%v", runs, len(candidates), lastErr)
	}
	run, passes = scripted([]ocrCandidate{{text: "ERR"}, {text: "ERR"}})
	if candidates, _, lastErr := runEnsemble(passes, run); len(candidates) != 0 || lastErr == nil {
		t.Fatal("all-failed ensemble must report the Tesseract error")
	}
}
