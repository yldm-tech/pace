package auth

import "testing"

// A value encrypted here decrypts back through the same path Django's reader uses, so a row written by this and read by the running server agree.
func TestTheConfigurationRoundTrip(t *testing.T) {
	const secret = "a-secret-key"
	for _, value := range []string{"a", "a client secret", "a value exactly sixteen", "Ünicode ☃"} {
		encrypted, err := EncryptConfiguration(value, secret)
		if err != nil {
			t.Fatalf("encrypting %q: %v", value, err)
		}
		decrypted, err := decryptDjangoConfiguration(encrypted, secret)
		if err != nil {
			t.Fatalf("decrypting %q: %v", value, err)
		}
		if decrypted != value {
			t.Errorf("%q round-tripped to %q", value, decrypted)
		}
	}
	// An empty value is stored as an empty string rather than as a token, which is what the python guard leaves behind.
	if encrypted, err := EncryptConfiguration("", secret); err != nil || encrypted != "" {
		t.Errorf("an empty value encrypted to %q (%v)", encrypted, err)
	}
	// A token signed with another key is refused rather than decrypted.
	encrypted, _ := EncryptConfiguration("a", secret)
	if _, err := decryptDjangoConfiguration(encrypted, "another-secret"); err == nil {
		t.Error("a token signed with another key decrypted")
	}
}
