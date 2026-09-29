package session

import "testing"

func TestPasswordPromptDetection(t *testing.T) {
	for _, q := range []string{"Password:", "user@host's password: ", "Senha:", "Enter passphrase", "Contraseña:"} {
		if !passwordPromptRe.MatchString(q) {
			t.Errorf("%q should be a password prompt", q)
		}
	}
	for _, q := range []string{"Verification code:", "OTP:", "Enter PIN for token:", "Duo two-factor login:"} {
		if passwordPromptRe.MatchString(q) {
			t.Errorf("%q must not be captured as a password", q)
		}
	}
}
