# iferr-analyzer

A Go analyzer that suggests inlining assignments into `if` conditions.

## Install

```
go install github.com/imjasonh/iferr-analyzer/cmd/iferr@latest
```

## Why?

Inlining error assignments into if statements prevents them from leaking into the outer scope. It also takes up less vertical space.

I find it useful to make it clearer to readers that the `err` is only meant to be considered during this block.
If another `err` is defined outside the conditional scope, its value will not be shadowed by the error when using `:=`.

I don't generally make a point of this style preference in code reviews (anymore...) but when I write code with my own human hands I tend to prefer this form. A big reason I don't attempt to enforce this style preference in code reviews is that (besides being subjective) it's not automatically enforceable. Well, now it is! I can put this in my pre-commit hooks and automatically satisfy my nittiest nit.

This was also just an excuse to get to play with Go's analyzer framework, which is neat.

## Usage

```
iferr ./...        # report diagnostics
iferr -fix ./...   # apply fixes
```

## Examples

```go
// Before
err := foo()
if err != nil {
		return err
}

// After
if err := foo(); err != nil {
	return err
}
```

```go
// Before
_, err := foo()
if err != nil {
	return err
}

// After
if _, err := foo(); err != nil {
	return err
}
```

```go
// Before
_, _, err := foo()
if err != nil {
	return err
}

// After
if _, _, err := foo(); err != nil {
	return err
}
```

```go
// Before
var err error
err = foo()
if err != nil {
	return err
}

// After
if err := foo(); err != nil {
	return err
}
```

Works with `== nil` too:

```go
// Before
err := foo()
if err == nil {
	fmt.Println("ok")
}

// After
if err := foo(); err == nil {
	fmt.Println("ok")
}
```

The analyzer will _not_ suggest inlining when a variable is used after the `if` block:

```go
// No diagnostic: result is used after the if
result, err := bar()
if err != nil {
	return 0, err
}
return result, nil
```

See [testdata/src/a/a.go](testdata/src/a/a.go) for exhaustive positive and negative examples.

### Testing

Go's analyzer framework comes with a nice package, `golang.org/x/tools/go/analysis/analysistest`, which runs on testdata files to flag positive and negative cases, and matches against a `.golden` file to test the behavior of `-fix`.

After writing the simplest version of this, I had Claude run the analyzer on a corpus of code in my local go mod cache, and it identified more cases, which I determined to be true positives, false positives, true negatives, and false negatives. Claude added test cases for each and modified the code until it passed. We went back and forth for a couple million tokens until it stabilized. There may be more cases; if you find one, let me know!
