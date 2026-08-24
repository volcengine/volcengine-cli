# Embedded go-jmespath

This directory is a source snapshot of
[`github.com/jmespath/go-jmespath`](https://github.com/jmespath/go-jmespath)
v0.4.0 (commit `3d4fd11601ddca248480565884e34e393313cd62`). It is kept inside the
main module so release binaries and `go install ...@version` use the same
interpreter fixes while the CLI remains compatible with Go 1.17.

Local fixes relative to v0.4.0:

- object value projections allocate a zero-length, pre-sized slice instead of
  evaluating synthetic `nil` elements;
- array `contains()` uses the interpreter's recursive equality semantics, as
  required by JMESPath, instead of Go interface comparability;
- `sort_by()` sorts a shallow copy instead of mutating the caller's response
  slice, so concurrent searches can safely share immutable input;
- `sort_by()`, `max_by()`, and `min_by()` validate an expression key even for
  a one-element array, keeping type errors independent of response length;
- JSON literals are decoded with `UseNumber()`, and numeric `==` / `!=` / `<` /
  `>` / `<=` / `>=` compare exact JSON decimal values instead of `float64`;
- `max` / `min` / `sum` / `avg` / `abs` / `ceil` / `floor` / `to_number` /
  `sort` and `*_by` over numbers use exact decimal comparison/arithmetic
  instead of `float64`. Results keep every digit of a finite decimal
  expansion; only `avg()` can produce a repeating decimal, which is rounded to
  at least `roundedSignificantDigits` significant digits. Arithmetic that would
  have to expand an exponent beyond `maxArithmeticExponent` into digits fails
  explicitly, while comparison, sorting, and `abs()` stay exact for any token.

Keep this package as a frozen upstream snapshot. Apply only relevant upstream
correctness or security fixes, and preserve this provenance note and LICENSE.
