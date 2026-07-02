package figma

import "testing"

func TestParseURLs(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantKey  string
		wantNode string
		wantN    int
	}{
		{
			"design-url-with-node",
			"Build the dashboard per https://www.figma.com/design/AbC123dEf456/My-App?node-id=12-345&t=xyz",
			"AbC123dEf456", "12:345", 1,
		},
		{
			"legacy-file-url",
			"see https://www.figma.com/file/Zz9YxW8vU7/Landing-Page",
			"Zz9YxW8vU7", "", 1,
		},
		{
			"node-id-colon-form",
			"https://www.figma.com/design/K1/App?node-id=7%3A21",
			"K1", "7:21", 1,
		},
		{
			"proto-url",
			"prototype at https://www.figma.com/proto/P9Q8r7/Flow?node-id=1-2",
			"P9Q8r7", "1:2", 1,
		},
		{"no-url", "Build a REST API for tasks", "", "", 0},
		{"non-figma-url", "see https://example.com/design/whatever", "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := ParseURLs(tt.text)
			if len(refs) != tt.wantN {
				t.Fatalf("want %d refs, got %d: %+v", tt.wantN, len(refs), refs)
			}
			if tt.wantN == 0 {
				return
			}
			if refs[0].FileKey != tt.wantKey {
				t.Errorf("key: want %q, got %q", tt.wantKey, refs[0].FileKey)
			}
			if refs[0].NodeID != tt.wantNode {
				t.Errorf("node: want %q, got %q", tt.wantNode, refs[0].NodeID)
			}
		})
	}
}

func TestParseURLs_DedupesAndKeepsOrder(t *testing.T) {
	text := "https://www.figma.com/design/AAA/x?node-id=1-1 then https://www.figma.com/design/BBB/y and again https://www.figma.com/design/AAA/x?node-id=1-1"
	refs := ParseURLs(text)
	if len(refs) != 2 {
		t.Fatalf("want 2 deduped refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].FileKey != "AAA" || refs[1].FileKey != "BBB" {
		t.Errorf("order not preserved: %+v", refs)
	}
}
