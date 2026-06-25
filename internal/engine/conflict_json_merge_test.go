package engine

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIsStructuredJSONMergeable(t *testing.T) {
	yes := []string{
		"package.json", "tsconfig.json", "jsconfig.json",
		"app/tsconfig.json", "tsconfig.build.json", "tsconfig.spec.json",
		"composer.json", ".eslintrc.json", "nest-cli.json",
	}
	for _, f := range yes {
		if !isStructuredJSONMergeable(f) {
			t.Errorf("%q should be structurally JSON-mergeable", f)
		}
	}
	no := []string{
		"package-lock.json", // lock file — handled separately, never merged
		"composer.lock",
		"main.ts", "README.md", ".gitignore", "Cargo.toml", "data.json.txt",
	}
	for _, f := range no {
		if isStructuredJSONMergeable(f) {
			t.Errorf("%q should NOT be structurally JSON-mergeable", f)
		}
	}
}

// The headline case: both sides add different dependencies and scripts to
// package.json. The correct resolution keeps BOTH — this is exactly what the
// LLM resolver kept failing to do (returning commentary), thrashing the story.
func TestStructuralJSONMerge_UnionsDependencies(t *testing.T) {
	ours := []byte(`{
  "name": "app",
  "version": "1.0.0",
  "dependencies": { "react": "^18.0.0", "left-pad": "^1.0.0" },
  "scripts": { "build": "tsc" }
}`)
	theirs := []byte(`{
  "name": "app",
  "version": "1.1.0",
  "dependencies": { "react": "^18.0.0", "zod": "^3.0.0" },
  "scripts": { "test": "vitest" }
}`)

	out, err := structuralJSONMerge(ours, theirs)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("merged output is not valid JSON: %v\n%s", err, out)
	}

	deps := got["dependencies"].(map[string]any)
	for _, want := range []string{"react", "left-pad", "zod"} {
		if _, ok := deps[want]; !ok {
			t.Errorf("merged dependencies missing %q (deps=%v)", want, deps)
		}
	}
	scripts := got["scripts"].(map[string]any)
	if _, ok := scripts["build"]; !ok {
		t.Error("merged scripts lost ours.build")
	}
	if _, ok := scripts["test"]; !ok {
		t.Error("merged scripts lost theirs.test")
	}
	// Scalar conflict (version): theirs (story side) wins.
	if got["version"] != "1.1.0" {
		t.Errorf("version = %v, want theirs 1.1.0", got["version"])
	}
}

func TestStructuralJSONMerge_TsconfigCompilerOptions(t *testing.T) {
	ours := []byte(`{"compilerOptions":{"strict":true,"outDir":"./dist"}}`)
	theirs := []byte(`{"compilerOptions":{"strict":true,"target":"ES2022"}}`)
	out, err := structuralJSONMerge(ours, theirs)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	co := got["compilerOptions"].(map[string]any)
	if co["outDir"] != "./dist" || co["target"] != "ES2022" || co["strict"] != true {
		t.Errorf("compilerOptions not unioned: %v", co)
	}
}

func TestStructuralJSONMerge_EmptySideKeepsOther(t *testing.T) {
	body := []byte(`{"name":"app"}`)
	if out, err := structuralJSONMerge(nil, body); err != nil || string(out) != string(body) {
		t.Errorf("empty ours: got %q err %v", out, err)
	}
	if out, err := structuralJSONMerge(body, []byte("  ")); err != nil || string(out) != string(body) {
		t.Errorf("empty theirs: got %q err %v", out, err)
	}
}

func TestStructuralJSONMerge_InvalidJSONErrors(t *testing.T) {
	if _, err := structuralJSONMerge([]byte(`{"a":1}`), []byte(`not json`)); err == nil {
		t.Fatal("expected error when a side is not valid JSON (so caller falls back to LLM)")
	}
}

func TestDeepMergeJSON_TheirsWinsOnTypeMismatch(t *testing.T) {
	// ours value is an object, theirs is a scalar at the same key → theirs wins.
	ours := map[string]any{"x": map[string]any{"deep": true}}
	theirs := map[string]any{"x": "scalar"}
	got := deepMergeJSON(ours, theirs)
	want := map[string]any{"x": "scalar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deepMergeJSON = %v, want %v", got, want)
	}
}
