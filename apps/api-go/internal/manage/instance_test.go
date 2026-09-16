package manage

import "testing"

// Every configuration variable the Django app seeds is in the embedded list, with the environment variable it reads and whether it is stored encrypted.
func TestTheInstanceConfigVariables(t *testing.T) {
	variables := instanceConfigVariables()
	if len(variables) < 30 {
		t.Fatalf("only %d configuration variables are embedded", len(variables))
	}
	byKey := map[string]configVariable{}
	for _, variable := range variables {
		if variable.Key == "" || variable.Category == "" {
			t.Errorf("a variable is missing its key or category: %#v", variable)
		}
		if _, seen := byKey[variable.Key]; seen {
			t.Errorf("%s is listed twice", variable.Key)
		}
		byKey[variable.Key] = variable
	}

	// The three that decide whether anybody can sign in at all.
	for key, want := range map[string]string{
		"ENABLE_SIGNUP": "1", "ENABLE_EMAIL_PASSWORD": "1", "ENABLE_MAGIC_LINK_LOGIN": "0",
	} {
		variable, known := byKey[key]
		if !known {
			t.Fatalf("%s is not in the list", key)
		}
		if variable.Default != want {
			t.Errorf("%s defaults to %q rather than %q", key, variable.Default, want)
		}
		if variable.Encrypted {
			t.Errorf("%s is stored encrypted, and it is not a secret", key)
		}
	}

	// At least one secret is stored encrypted, which is what the encryption path exists for.
	encrypted := 0
	for _, variable := range variables {
		if variable.Encrypted {
			encrypted++
		}
	}
	if encrypted == 0 {
		t.Error("no configuration variable is stored encrypted")
	}
}

// The instance identifier is twenty-four hex characters, which is what secrets.token_hex(12) gives.
func TestTheInstanceIdentifier(t *testing.T) {
	first, err := instanceIdentifier()
	if err != nil {
		t.Fatalf("building an identifier: %v", err)
	}
	if len(first) != 24 {
		t.Errorf("the identifier is %d characters: %q", len(first), first)
	}
	second, _ := instanceIdentifier()
	if first == second {
		t.Error("two identifiers came out the same")
	}
}
