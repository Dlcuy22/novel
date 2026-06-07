// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// helpers.go: small parsing utilities.
//
// Purpose:
//   Shared helpers that don't belong to a single parse stage: re-lexing source
//   fragments (for interpolation) and tolerant numeric-literal parsing that
//   ignores Novel's type suffixes.

package parser

import (
	"strconv"

	"github.com/dlcuy22/novel/internal/lexer"
	"github.com/dlcuy22/novel/internal/token"
)

// tokensOf lexes a source fragment into a token slice (used to parse embedded
// interpolation expressions).
func tokensOf(src string) []token.Token {
	return lexer.New(src).Tokenize()
}

// atoiSafe parses a (possibly suffixed) integer literal, ignoring suffixes like
// "u" or "i64"; returns 0 on failure.
func atoiSafe(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}
