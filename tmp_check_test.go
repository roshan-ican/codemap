package main

import (
  "fmt"
  "testing"
)

func TestTmpCheck(t *testing.T) {
  g, err := analyzeRepositoryGraph(".")
  if err != nil { t.Fatal(err) }
  for _, e := range g.Edges {
    if string(e.From) == "frontend/src/App.svelte" || string(e.To) == "frontend/src/App.svelte" || string(e.From) == "frontend/src/CodeCard.svelte" || string(e.To) == "frontend/src/CodeCard.svelte" {
      fmt.Println("EDGE", e.From, "->", e.To, e.Kind)
    }
  }
}
