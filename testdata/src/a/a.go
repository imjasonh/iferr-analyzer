package a

import "fmt"

func foo() error {
	return nil
}

func bar() (int, error) {
	return 0, nil
}

func baz() (int, string, error) {
	return 0, "", nil
}

type thing struct {
	val int
}

// Basic case
func basic() error {
	err := foo() // want `can inline assignment into if statement`
	if err != nil {
		return err
	}
	return nil
}

// Blank + err
func blankErr() error {
	_, err := bar() // want `can inline assignment into if statement`
	if err != nil {
		return err
	}
	return nil
}

// Multi-line if body
func multiLineBody() error {
	err := foo() // want `can inline assignment into if statement`
	if err != nil {
		fmt.Println("error:", err)
		return err
	}
	return nil
}

// Multiple blanks + err
func multipleBlanks() error {
	_, _, err := baz() // want `can inline assignment into if statement`
	if err != nil {
		return err
	}
	return nil
}

// Non-err variable name
func namedErr() error {
	_, ferr := bar() // want `can inline assignment into if statement`
	if ferr != nil {
		return ferr
	}
	return nil
}

// err == nil
func equalNilPositive() {
	err := foo() // want `can inline assignment into if statement`
	if err == nil {
		fmt.Println("success")
	}
}

// Assignment with = and preceding var declaration
func assignWithVar() error {
	var err error
	err = foo() // want `can inline assignment into if statement`
	if err != nil {
		return err
	}
	return nil
}

// Comment on assignment line only
func commentOnAssignLine() error {
	err := foo() /* hi */ // want `can inline assignment into if statement`
	if err != nil {
		return err
	}
	return nil
}

// Comment on if line only
func commentOnIfLine() error {
	err := foo() // want `can inline assignment into if statement`
	if err != nil { // hi2
		return err
	}
	return nil
}

// Comments on both assignment and if lines
func commentOnBothLines() error {
	err := foo() /* hi */ // want `can inline assignment into if statement`
	if err != nil { // hi2
		return err
	}
	return nil
}

// Comment between assignment and if
func commentBetweenStmts() error {
	err := foo() // want `can inline assignment into if statement`
	// this checks for errors
	if err != nil {
		return err
	}
	return nil
}

// --- Negative cases below ---

// err used after if block
func errUsedAfter() error {
	err := foo()
	if err != nil {
		return err
	}
	fmt.Println(err)
	return nil
}

// If already has an Init
func alreadyHasInit() error {
	err := foo()
	if err2 := foo(); err2 != nil {
		_ = err
		return err2
	}
	return nil
}

// err used after if block (== nil variant)
func equalNilUsedAfter() error {
	err := foo()
	if err == nil {
		return nil
	}
	return err
}

// Condition compares to non-nil value
func notNilComparison() error {
	err := foo()
	other := foo()
	if err != other {
		return err
	}
	return nil
}

// Non-consecutive statements
func nonConsecutive() error {
	err := foo()
	fmt.Println("between")
	if err != nil {
		return err
	}
	return nil
}

// result used after the if block
func resultUsedAfter() (int, error) {
	result, err := bar()
	if err != nil {
		return 0, err
	}
	return result, nil
}

// Selector expression on LHS: := is invalid
func selectorLHS() error {
	var t thing
	var err error
	t.val, err = bar()
	if err != nil {
		return err
	}
	_ = t
	return nil
}

// Named return parameter: bare return implicitly uses it
func namedReturn() (err error) {
	err = foo()
	if err != nil {
		fmt.Println(err)
	}
	return
}

// Variable used in outer scope after nested block
func usedInOuterScope() error {
	var err error
	if true {
		err = foo()
		if err != nil {
			return err
		}
	}
	fmt.Println(err)
	return nil
}

// Variable used in for loop condition (re-evaluated after body)
func usedInForCondition() error {
	data := []int{1, 2, 3}
	i, err := bar()
	if err != nil {
		return err
	}
	for i < len(data) {
		i, err = bar()
		if err != nil {
			return err
		}
	}
	_ = data
	return nil
}

// Named return in function literal: bare return implicitly uses it
func funcLitNamedReturn() {
	f := func() (err error) {
		err = foo()
		if err != nil {
			fmt.Println(err)
		}
		return
	}
	_ = f
}

// result used after the if block (with blanks)
func resultBlanksUsedAfter() (int, error) {
	result, _, err := baz()
	if err != nil {
		return 0, err
	}
	return result, nil
}
