package types

import (
	"strings"
	"testing"

	"github.com/dlcuy22/novel/internal/parser"
)

// check parses src (failing on parse errors) and returns the type errors.
func check(t *testing.T, src string) []Error {
	t.Helper()
	p := parser.New(src)
	f := p.ParseFile()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	c := New()
	c.Check(f)
	return c.Errors()
}

// wantClean asserts no type errors were reported.
func wantClean(t *testing.T, src string) {
	t.Helper()
	if errs := check(t, src); len(errs) != 0 {
		t.Fatalf("expected no type errors, got: %v", errs)
	}
}

// wantError asserts at least one error whose message contains substr.
func wantError(t *testing.T, src, substr string) {
	t.Helper()
	errs := check(t, src)
	for _, e := range errs {
		if strings.Contains(e.Msg, substr) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got: %v", substr, errs)
}

func TestValidProgramIsClean(t *testing.T) {
	wantClean(t, `import std.io

type Rect struct {
    pub width  float64
    pub height float64
}

fn (r Rect) area() float64 {
    ret r.width * r.height
}

fn add(a int, b int) int {
    ret a + b
}

fn main() {
    let r = Rect{ width: 3.0, height: 4.0 }
    let s = add(1, 2)
    io.println($"{r.area()} and {s}")
}`)
}

func TestUnusedLocalIsError(t *testing.T) {
	wantError(t, `fn main() {
    let x = 5
}`, "declared and not used: x")
}

func TestUsedOnlyInInterpolationIsClean(t *testing.T) {
	// A var referenced only inside $"...{x}..." must count as used.
	wantClean(t, `import std.io
fn main() {
    let name = "world"
    io.println($"hello {name}")
}`)
}

func TestBlankNameNeverUnused(t *testing.T) {
	wantClean(t, `import std.io
fn main() {
    let xs = [1, 2, 3]
    for _, v in xs {
        io.println($"{v}")
    }
}`)
}

func TestUndefinedIdentifierIsError(t *testing.T) {
	wantError(t, `import std.io
fn main() {
    io.println($"{missing}")
}`, "undefined: missing")
}

func TestConstReassignmentIsError(t *testing.T) {
	wantError(t, `import std.io
fn main() {
    const limit = 10
    limit = 20
    io.println($"{limit}")
}`, "cannot assign to constant: limit")
}

func TestConstIncDecIsError(t *testing.T) {
	wantError(t, `import std.io
fn main() {
    const limit = 10
    limit++
    io.println($"{limit}")
}`, "cannot assign to constant: limit")
}

func TestWrongArityTooFew(t *testing.T) {
	wantError(t, `fn add(a int, b int) int {
    ret a + b
}
fn main() {
    add(1)
}`, "want 2")
}

func TestWrongArityTooMany(t *testing.T) {
	wantError(t, `fn add(a int, b int) int {
    ret a + b
}
fn main() {
    add(1, 2, 3)
}`, "want 2")
}

func TestForwardReferenceResolves(t *testing.T) {
	// A function may call one declared later in the file.
	wantClean(t, `import std.io
fn main() {
    io.println($"{helper(2)}")
}
fn helper(n int) int {
    ret n * 2
}`)
}

func TestUndefinedTypeInLetIsError(t *testing.T) {
	wantError(t, `fn main() {
    let x Widget = nil
    use(x)
}
fn use(w Widget) {
}`, "undefined type: Widget")
}

func TestUndefinedStructLiteralTypeIsError(t *testing.T) {
	wantError(t, `fn main() {
    let p = Point{ x: 1, y: 2 }
    sink(p)
}
fn sink(p int) {
}`, "undefined type: Point")
}

func TestBangPropagationOutsideErrorFnIsError(t *testing.T) {
	wantError(t, `fn risky() (int, error) {
    ret 0, error("boom")
}
fn caller() int {
    let v = risky()!
    ret v
}`, "'!' propagation")
}

func TestBangPropagationInErrorFnIsClean(t *testing.T) {
	wantClean(t, `fn risky() (int, error) {
    ret 0, error("boom")
}
fn caller() (int, error) {
    let v = risky()!
    ret v, nil
}`)
}

func TestShadowingAcrossScopesIsClean(t *testing.T) {
	wantClean(t, `import std.io
fn main() {
    let x = 1
    if x > 0 {
        let x = 2
        io.println($"{x}")
    }
    io.println($"{x}")
}`)
}

func TestParametersAndReceiversExemptFromUnused(t *testing.T) {
	wantClean(t, `type Box struct {
    v int
}
fn (b Box) noop() {
}
fn ignore(a int, b int) {
}
fn main() {
    let x = Box{ v: 1 }
    x.noop()
    ignore(1, 2)
}`)
}

func TestVariadicArityAcceptsZeroOrMore(t *testing.T) {
	wantClean(t, `fn sum(nums ...int) int {
    ret 0
}
fn main() {
    sum()
    sum(1)
    sum(1, 2, 3)
}`)
}

func TestBuiltinsNeedNoDeclaration(t *testing.T) {
	wantClean(t, `fn producer(out chan<int>) {
    send(out, 1)
    close(out)
}
fn main() {
    let ch = chan<int>(1)
    spawn producer(ch)
    let v = recv(ch)
    let e = error("x")
    use(v, e)
}
fn use(v int, e error) {
}`)
}

func TestAnyTypeIsAccepted(t *testing.T) {
	// `any` is the dynamic escape hatch for values the checker can't track
	// (e.g. a table from a Lua stdlib module). It is valid in params, results,
	// annotations, and composite element positions.
	wantClean(t, `fn handle(req any) any {
    ret req
}
fn main() {
    let xs []any = []any{}
    let r = handle(xs)
    sink(r)
}
fn sink(v any) {
}`)
}
