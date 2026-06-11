package cli

import "testing"

func TestRedactDSNPassword_URLForm(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			"postgres://vxd:secret@localhost:5432/db",
			"postgres://vxd:***@localhost:5432/db",
		},
		{
			"postgresql://user:p@ss@host/db",
			"postgresql://user:***@host/db",
		},
		{
			"postgres://user@host/db", // no password — leave as-is
			"postgres://user@host/db",
		},
		{
			"postgres://host/db", // no auth
			"postgres://host/db",
		},
	}
	for _, c := range cases {
		if got := redactDSNPassword(c.in); got != c.want {
			t.Errorf("redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactDSNPassword_KeyValueForm(t *testing.T) {
	in := "host=localhost user=vxd password=secret dbname=foo"
	want := "host=localhost user=vxd password=*** dbname=foo"
	if got := redactDSNPassword(in); got != want {
		t.Errorf("redact = %q, want %q", got, want)
	}
}

func TestRedactDSNPassword_DoesNotMatchInPath(t *testing.T) {
	// A path or db name that contains "password" must not trigger.
	// `password=` is the matcher — a path like /password_history is safe.
	in := "postgres://user:secret@host/password_history"
	want := "postgres://user:***@host/password_history"
	if got := redactDSNPassword(in); got != want {
		t.Errorf("redact = %q, want %q", got, want)
	}
}
