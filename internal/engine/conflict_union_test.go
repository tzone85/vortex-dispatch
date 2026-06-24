package engine

import "testing"

func TestIsUnionMergeableConfig(t *testing.T) {
	yes := []string{".gitignore", "sub/dir/.gitignore", ".dockerignore", ".npmignore", ".eslintignore", ".prettierignore", ".gitattributes"}
	no := []string{"main.go", "package.json", "src/index.ts", "README.md", "go.mod", "tsconfig.json"}
	for _, f := range yes {
		if !isUnionMergeableConfig(f) {
			t.Errorf("isUnionMergeableConfig(%q) = false, want true", f)
		}
	}
	for _, f := range no {
		if isUnionMergeableConfig(f) {
			t.Errorf("isUnionMergeableConfig(%q) = true, want false", f)
		}
	}
}

func TestUnionResolveConflict(t *testing.T) {
	// A conflicted .gitignore: both sides added entries; "node_modules" is on
	// both sides. The union must keep every unique line, drop conflict markers
	// and the duplicate, and preserve first-seen order.
	conflicted := "node_modules\n" +
		"<<<<<<< HEAD\n" +
		"dist/\n" +
		"*.log\n" +
		"=======\n" +
		"node_modules\n" +
		"coverage/\n" +
		">>>>>>> story\n" +
		".env\n"

	got := unionResolveConflict(conflicted)
	want := "node_modules\ndist/\n*.log\ncoverage/\n.env"
	if got != want {
		t.Errorf("unionResolveConflict mismatch\n got: %q\nwant: %q", got, want)
	}
	// Result must contain no conflict markers.
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"} {
		if containsLine(got, marker) {
			t.Errorf("union result still contains marker %q:\n%s", marker, got)
		}
	}
}

func containsLine(s, sub string) bool {
	return len(s) > 0 && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
