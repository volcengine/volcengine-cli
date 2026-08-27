package jmespath

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// CanonicalJSONNumber normalizes a JSON number without expanding its decimal
// exponent. For example, 1, 1.0 and 10e-1 all become "1e0". Only the exponent
// arithmetic uses big.Int, so a compact token such as 1e1000000000 stays compact
// in time and memory instead of allocating a billion-digit numerator.
func CanonicalJSONNumber(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}

	i := 0
	negative := false
	if raw[i] == '-' {
		negative = true
		i++
		if i == len(raw) {
			return "", false
		}
	}

	integerStart := i
	if raw[i] == '0' {
		i++
		if i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			return "", false
		}
	} else if raw[i] >= '1' && raw[i] <= '9' {
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
	} else {
		return "", false
	}
	integerEnd := i

	fractionStart, fractionEnd := i, i
	if i < len(raw) && raw[i] == '.' {
		i++
		fractionStart = i
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
		fractionEnd = i
		if fractionStart == fractionEnd {
			return "", false
		}
	}

	exponent := new(big.Int)
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		i++
		exponentNegative := false
		if i < len(raw) && (raw[i] == '+' || raw[i] == '-') {
			exponentNegative = raw[i] == '-'
			i++
		}
		exponentStart := i
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
		if exponentStart == i {
			return "", false
		}
		if _, ok := exponent.SetString(raw[exponentStart:i], 10); !ok {
			return "", false
		}
		if exponentNegative {
			exponent.Neg(exponent)
		}
	}
	if i != len(raw) {
		return "", false
	}

	digits := make([]byte, 0, integerEnd-integerStart+fractionEnd-fractionStart)
	digits = append(digits, raw[integerStart:integerEnd]...)
	digits = append(digits, raw[fractionStart:fractionEnd]...)
	first := 0
	for first < len(digits) && digits[first] == '0' {
		first++
	}
	if first == len(digits) {
		return "0", true
	}
	last := len(digits)
	for last > first && digits[last-1] == '0' {
		last--
	}

	exponent.Sub(exponent, big.NewInt(int64(fractionEnd-fractionStart)))
	exponent.Add(exponent, big.NewInt(int64(len(digits)-last)))
	var canonical strings.Builder
	canonical.Grow(last - first + len(exponent.String()) + 2)
	if negative {
		canonical.WriteByte('-')
	}
	canonical.Write(digits[first:last])
	canonical.WriteByte('e')
	canonical.WriteString(exponent.String())
	return canonical.String(), true
}

func canonicalNumberToken(value interface{}) (string, bool) {
	switch n := value.(type) {
	case json.Number:
		return CanonicalJSONNumber(n.String())
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return "", false
		}
		if n == 0 {
			return "0", true
		}
		return CanonicalJSONNumber(strconv.FormatFloat(n, 'g', -1, 64))
	default:
		return "", false
	}
}

// compareNumbers compares JSON numbers, exact-number wrappers, and float64
// values by their decimal value. ok is false when either side is not a number.
func compareNumbers(left, right interface{}) (int, bool) {
	leftToken, ok := canonicalNumberToken(left)
	if !ok {
		return 0, false
	}
	rightToken, ok := canonicalNumberToken(right)
	if !ok {
		return 0, false
	}
	if leftToken == rightToken {
		return 0, true
	}
	leftNum, ok := parseCanonicalNumber(leftToken)
	if !ok {
		return 0, false
	}
	rightNum, ok := parseCanonicalNumber(rightToken)
	if !ok {
		return 0, false
	}
	return compareCanonicalNumbers(leftNum, rightNum), true
}

type canonicalNumber struct {
	neg    bool
	zero   bool
	digits string
	exp    *big.Int
}

func parseCanonicalNumber(canonical string) (canonicalNumber, bool) {
	if canonical == "" {
		return canonicalNumber{}, false
	}
	if canonical == "0" {
		return canonicalNumber{zero: true, exp: new(big.Int)}, true
	}
	i := 0
	neg := false
	if canonical[i] == '-' {
		neg = true
		i++
	}
	e := strings.IndexByte(canonical[i:], 'e')
	if e < 0 {
		return canonicalNumber{}, false
	}
	digits := canonical[i : i+e]
	exp := new(big.Int)
	if _, ok := exp.SetString(canonical[i+e+1:], 10); !ok || digits == "" {
		return canonicalNumber{}, false
	}
	return canonicalNumber{neg: neg, digits: digits, exp: exp}, true
}

func isNumberValue(value interface{}) bool {
	_, ok := canonicalNumberToken(value)
	return ok
}

func arrayNumberValues(data interface{}) ([]interface{}, bool) {
	arr, ok := data.([]interface{})
	if !ok {
		return nil, false
	}
	for _, item := range arr {
		if !isNumberValue(item) {
			return nil, false
		}
	}
	return arr, true
}

// maxArithmeticExponent bounds the decimal exponent that arithmetic expands into
// digits. Refusing 1e1000000000 keeps a compact token from allocating a
// billion-digit numerator; comparison and sorting stay exact without expanding.
var maxArithmeticExponent = big.NewInt(10000)

func numberToRat(value interface{}) (*big.Rat, error) {
	token, ok := canonicalNumberToken(value)
	if !ok {
		return nil, errors.New("invalid type, expected number")
	}
	parsed, ok := parseCanonicalNumber(token)
	if !ok {
		return nil, errors.New("invalid type, expected number")
	}
	if parsed.zero {
		return new(big.Rat), nil
	}
	if parsed.exp.CmpAbs(maxArithmeticExponent) > 0 {
		return nil, fmt.Errorf("number %s is too large for exact arithmetic", token)
	}
	significand := new(big.Int)
	if _, ok := significand.SetString(parsed.digits, 10); !ok {
		return nil, errors.New("invalid type, expected number")
	}
	if parsed.neg {
		significand.Neg(significand)
	}
	power := new(big.Int).Exp(big.NewInt(10), new(big.Int).Abs(parsed.exp), nil)
	result := new(big.Rat)
	if parsed.exp.Sign() >= 0 {
		result.SetInt(new(big.Int).Mul(significand, power))
	} else {
		result.SetFrac(significand, power)
	}
	return result, nil
}

func integerFromNumber(value interface{}, ceil bool) (json.Number, error) {
	r, err := numberToRat(value)
	if err != nil {
		return "", err
	}
	if r.IsInt() {
		return ratToJSONNumber(r), nil
	}
	quo := new(big.Int)
	rem := new(big.Int)
	quo.QuoRem(r.Num(), r.Denom(), rem)
	if rem.Sign() == 0 {
		return json.Number(quo.String()), nil
	}
	if ceil {
		if r.Sign() > 0 {
			quo.Add(quo, big.NewInt(1))
		}
	} else if r.Sign() < 0 {
		quo.Sub(quo, big.NewInt(1))
	}
	return json.Number(quo.String()), nil
}

// ratToJSONNumber renders a rational as a JSON number token. Every value with a
// finite decimal expansion is rendered exactly, however many digits that takes;
// a fixed digit budget would silently round 1e-50 to 0. Only a repeating decimal
// is rounded, and just avg() can produce one.
func ratToJSONNumber(value *big.Rat) json.Number {
	if value.IsInt() {
		return json.Number(value.Num().String())
	}
	digits, exact := exactFractionDigits(value.Denom())
	if !exact {
		digits = roundedFractionDigits(value)
	}
	text := value.FloatString(digits)
	if strings.ContainsRune(text, '.') {
		text = strings.TrimRight(text, "0")
		text = strings.TrimRight(text, ".")
	}
	return json.Number(text)
}

// exactFractionDigits reports how many fractional digits write a rational with
// this denominator exactly. A denominator with a prime factor other than 2 or 5
// has no finite decimal expansion.
func exactFractionDigits(denom *big.Int) (int, bool) {
	rest := new(big.Int).Set(denom)
	twos := rest.TrailingZeroBits()
	rest.Rsh(rest, twos)

	fives := uint(0)
	five, quotient, remainder := big.NewInt(5), new(big.Int), new(big.Int)
	for {
		quotient.QuoRem(rest, five, remainder)
		if remainder.Sign() != 0 {
			break
		}
		rest.Set(quotient)
		fives++
	}
	if rest.Cmp(big.NewInt(1)) != 0 {
		return 0, false
	}
	if twos > fives {
		return int(twos), true
	}
	return int(fives), true
}

// roundedSignificantDigits keeps a repeating decimal meaningful for magnitudes
// far below 1, which a fixed fractional-digit budget would round to zero.
const roundedSignificantDigits = 34

func roundedFractionDigits(value *big.Rat) int {
	leadingZeros := len(value.Denom().String()) - len(new(big.Int).Abs(value.Num()).String())
	if leadingZeros < 0 {
		leadingZeros = 0
	}
	return leadingZeros + roundedSignificantDigits
}

func compareCanonicalNumbers(left, right canonicalNumber) int {
	if left.zero && right.zero {
		return 0
	}
	if left.zero {
		if right.neg {
			return 1
		}
		return -1
	}
	if right.zero {
		if left.neg {
			return -1
		}
		return 1
	}
	if left.neg != right.neg {
		if left.neg {
			return -1
		}
		return 1
	}
	leftMag := new(big.Int).Set(left.exp)
	leftMag.Add(leftMag, big.NewInt(int64(len(left.digits)-1)))
	rightMag := new(big.Int).Set(right.exp)
	rightMag.Add(rightMag, big.NewInt(int64(len(right.digits)-1)))
	if cmp := leftMag.Cmp(rightMag); cmp != 0 {
		if left.neg {
			return -cmp
		}
		return cmp
	}
	leftDigits, rightDigits := left.digits, right.digits
	if pad := len(rightDigits) - len(leftDigits); pad > 0 {
		leftDigits += strings.Repeat("0", pad)
	} else if pad < 0 {
		rightDigits += strings.Repeat("0", -pad)
	}
	cmp := strings.Compare(leftDigits, rightDigits)
	if left.neg {
		return -cmp
	}
	return cmp
}
