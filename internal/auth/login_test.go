package auth

import "testing"

func TestClassifyLoginFailureDistinguishesCaptchaFromCredentials(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "captcha", body: `alertDiyalog("Hata","Güvenlik resmi hatalı","error")`, want: "invalid_captcha"},
		{name: "credentials", body: `alertDiyalog("Hata","Kullanıcı Doğrulama Başarısız!!--","error")`, want: "invalid_credentials"},
		{name: "unknown", body: `<form action="/auth/login/ln/tr"></form>`, want: "login_rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLoginFailure(tt.body); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
