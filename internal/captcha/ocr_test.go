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
