// quest-compiler — compile a Quest Editor scenario graph (scenario-graph/v1
// JSON) into a Lua scenario script. Phase 5.3.
//
// Usage:
//
//	quest-compiler -in story.json -out story.lua
//	cat story.json | quest-compiler            # stdin -> stdout
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/boneysan/ryzom/go-services/internal/questc"
)

func main() {
	in := flag.String("in", "", "input scenario-graph JSON (default: stdin)")
	out := flag.String("out", "", "output Lua file (default: stdout)")
	flag.Parse()

	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "quest-compiler:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	var raw []byte
	var err error
	if in == "" || in == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(in)
	}
	if err != nil {
		return err
	}

	var s questc.Storyline
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	lua, err := questc.Compile(&s)
	if err != nil {
		return err
	}

	if out == "" || out == "-" {
		_, err = os.Stdout.WriteString(lua)
		return err
	}
	return os.WriteFile(out, []byte(lua), 0o644)
}
