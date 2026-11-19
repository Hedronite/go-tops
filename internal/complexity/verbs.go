package complexity

import (
	"strconv"
	"strings"
)

// wrapOperands returns the zero-based operand index of each %w verb in a
// printf format, following the rules of package fmt for flags, width,
// precision, `*` and explicit argument indexes (`%[2]w`). A format it cannot
// parse yields none, so an unreadable format never earns the discount.
func wrapOperands(format string) []int {
	var ops []int
	arg := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		d, ok := scanDirective(format, i+1, arg)
		if !ok {
			return nil
		}
		if d.verb == 'w' {
			ops = append(ops, d.operand)
		}
		i, arg = d.end, d.next
	}
	return ops
}

// directive is one parsed `%...verb`.
type directive struct {
	verb    byte
	operand int // the operand the verb formats; -1 for %%
	end     int // index of the verb byte
	next    int // operand index after the directive
}

func scanDirective(f string, i, arg int) (directive, bool) {
	i = skipSet(f, i, "#0+- ")
	var ok bool
	if i, arg, ok = argIndex(f, i, arg); !ok {
		return directive{}, false
	}
	i, arg = starOrDigits(f, i, arg)
	if i < len(f) && f[i] == '.' {
		if i, arg, ok = argIndex(f, i+1, arg); !ok {
			return directive{}, false
		}
		i, arg = starOrDigits(f, i, arg)
	}
	if i, arg, ok = argIndex(f, i, arg); !ok || i >= len(f) {
		return directive{}, false
	}
	if f[i] == '%' {
		return directive{verb: '%', operand: -1, end: i, next: arg}, true
	}
	return directive{verb: f[i], operand: arg, end: i, next: arg + 1}, true
}

func skipSet(f string, i int, set string) int {
	for i < len(f) && strings.IndexByte(set, f[i]) >= 0 {
		i++
	}
	return i
}

// starOrDigits skips a width or precision: `*` takes an operand, digits take none.
func starOrDigits(f string, i, arg int) (int, int) {
	if i < len(f) && f[i] == '*' {
		return i + 1, arg + 1
	}
	return skipSet(f, i, "0123456789"), arg
}

// argIndex reads an optional `[n]` and moves the operand index to n-1. It
// fails on a malformed index.
func argIndex(f string, i, arg int) (int, int, bool) {
	if i >= len(f) || f[i] != '[' {
		return i, arg, true
	}
	end := strings.IndexByte(f[i:], ']')
	if end < 0 {
		return i, arg, false
	}
	n, err := strconv.Atoi(f[i+1 : i+end])
	if err != nil || n < 1 {
		return i, arg, false
	}
	return i + end + 1, n - 1, true
}
