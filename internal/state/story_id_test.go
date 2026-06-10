package state

import "testing"

func TestValidateStoryID_Valid(t *testing.T) {
	valid := []string{
		"s-001",
		"STR-AUTH-1",
		"story_42",
		"a.b.c",
		"abc123",
		"s-001-child",
	}
	for _, id := range valid {
		if err := ValidateStoryID(id); err != nil {
			t.Errorf("ValidateStoryID(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidateStoryID_RejectsTraversalAndInjection(t *testing.T) {
	bad := []string{
		"",                     // empty
		".",                    // current dir
		"..",                   // parent dir
		"../../etc/passwd",     // path traversal
		"../sibling",           // path traversal
		"foo/bar",              // path separator
		"foo\\bar",             // windows separator
		"-rf",                  // leading dash (arg injection)
		"--force",              // flag injection
		"a b",                  // whitespace
		"a;b",                  // shell metachar
		"a$(whoami)",           // command substitution
		"a`id`",                // backtick
		"a|b",                  // pipe
		"a&b",                  // background
		"a\nb",                 // newline
		"id with spaces",       // whitespace
	}
	for _, id := range bad {
		if err := ValidateStoryID(id); err == nil {
			t.Errorf("ValidateStoryID(%q) = nil, want error", id)
		}
	}
}

func TestValidateStoryID_RejectsOverLong(t *testing.T) {
	long := make([]byte, maxStoryIDLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateStoryID(string(long)); err == nil {
		t.Errorf("ValidateStoryID(<%d chars>) = nil, want error", maxStoryIDLen+1)
	}
}
