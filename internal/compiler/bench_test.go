// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCompileSingleFile benchmarks the transpiler speed for a single file.
// It uses Conway's Game of Life example, which is representative of typical
// Novel source code with variables, loops, slices, interpolation, and functions.
func BenchmarkCompileSingleFile(b *testing.B) {
	path := filepath.Join("..", "..", "examples", "game_of_life.nv")
	src, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("failed to read benchmark file: %v", err)
	}

	srcStr := string(src)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res := Compile(srcStr)
		if res.HasErrors() {
			b.Fatal("compilation failed during benchmark")
		}
	}
}

// BenchmarkCompileFileE2E benchmarks the full graph-resolving compilation
// pipeline for a file (resolving dependencies, parsing, and bundling).
func BenchmarkCompileFileE2E(b *testing.B) {
	path := filepath.Join("..", "..", "examples", "game_of_life.nv")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res := CompileFile(path)
		if res.HasErrors() {
			b.Fatal("compilation failed during benchmark")
		}
	}
}
