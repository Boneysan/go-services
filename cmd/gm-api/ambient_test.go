package main

import "testing"

func TestParseAmbientLines(t *testing.T) {
	raw := "```json\n" + `{"lines":["The forge runs hot today.","  ","Fyros steel is not cheap, outlander."]}` + "\n```"
	lines, err := parseAmbientLines(raw)
	if err != nil {
		t.Fatalf("parseAmbientLines: %v", err)
	}
	if len(lines) != 2 { // the blank line is dropped
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if lines[1] != "Fyros steel is not cheap, outlander." {
		t.Errorf("line not trimmed/carried: %q", lines[1])
	}

	for _, bad := range []string{`nonsense`, `{"lines":[]}`, `{"lines":["   "]}`} {
		if _, err := parseAmbientLines(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
