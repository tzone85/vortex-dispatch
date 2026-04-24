package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsHallucinationLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"Looking at the user's request, they provided the file content", true},
		{"I'll resolve the conflicts to maintain consistency:", true},
		{"Here's the solution:", true},
		{"Based on the existing cxOp function", true},
		{"Now let me fix the remaining issues:", true},
		{"**Key Resolution Strategies:**", true},
		{"1. **Combined Imports**: Merged all unique imports", true},
		{"Sure, here is the updated code:", true},

		// NOT hallucination — actual code
		{"import React from 'react'", false},
		{"export function hello() {}", false},
		{"const x = 42;", false},
		{"// This is a comment", false},
		{"package main", false},
		{"def foo():", false},
	}

	for _, tc := range cases {
		got := isHallucinationLine(tc.line)
		if got != tc.want {
			t.Errorf("isHallucinationLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestScrubFile(t *testing.T) {
	dir := t.TempDir()

	// File with hallucination preamble
	path := filepath.Join(dir, "test.ts")
	content := `Looking at this merge conflict, I can see the newer branch has much more comprehensive functionality.

import React from 'react'
export function App() { return <div>Hello</div> }
`
	os.WriteFile(path, []byte(content), 0644)

	modified := scrubFile(path)
	if !modified {
		t.Fatal("expected scrubFile to return true for file with hallucination")
	}

	result, _ := os.ReadFile(path)
	if got := string(result); got != "import React from 'react'\nexport function App() { return <div>Hello</div> }\n" {
		t.Errorf("scrubbed content = %q", got)
	}
}

func TestScrubFile_NoHallucination(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "clean.ts")
	content := `import React from 'react'
export function App() { return <div>Hello</div> }
`
	os.WriteFile(path, []byte(content), 0644)

	modified := scrubFile(path)
	if modified {
		t.Fatal("expected scrubFile to return false for clean file")
	}
}

func TestScrubFile_EntirelyHallucination(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "bad.ts")
	content := `I've successfully resolved the merge conflict.
**Key Resolution Strategies:**
1. **Combined Imports**: Merged all unique imports
`
	os.WriteFile(path, []byte(content), 0644)

	modified := scrubFile(path)
	if modified {
		t.Fatal("expected scrubFile to skip entirely-hallucination file")
	}
}

func TestIsSourceExt(t *testing.T) {
	cases := map[string]bool{
		".ts":   true,
		".tsx":  true,
		".js":   true,
		".go":   true,
		".py":   true,
		".json": true,
		".md":   false,
		".png":  false,
		".pdf":  false,
		"":      false,
	}
	for ext, want := range cases {
		if got := isSourceExt(ext); got != want {
			t.Errorf("isSourceExt(%q) = %v, want %v", ext, got, want)
		}
	}
}

func TestScanFileForConflictMarkers(t *testing.T) {
	dir := t.TempDir()

	// File with conflict markers
	bad := filepath.Join(dir, "conflict.ts")
	os.WriteFile(bad, []byte("line1\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n"), 0644)
	if !scanFileForConflictMarkers(bad) {
		t.Error("expected conflict markers to be detected")
	}

	// Clean file
	good := filepath.Join(dir, "clean.ts")
	os.WriteFile(good, []byte("import React from 'react'\nexport default App\n"), 0644)
	if scanFileForConflictMarkers(good) {
		t.Error("expected no conflict markers in clean file")
	}
}
