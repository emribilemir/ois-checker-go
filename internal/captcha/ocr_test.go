package captcha

import "testing"

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
