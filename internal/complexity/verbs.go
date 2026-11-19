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

// maxOperand bounds an operand index. A call with more operands than this is not
// one a format can name, so an index past it, or one that overflows an int, makes
// the format unreadable: `%[9223372036854775807]*.*w` must not wrap round to a
// negative operand.
const maxOperand = 1 << 16

// directive is one parsed `%...verb`.
type directive struct {
	verb    byte
	operand int // the operand the verb formats; -1 for %%
	end     int // index of the verb byte
	next    int // operand index after the directive
}

// scanner reads one directive. The first thing it cannot read sets bad, and
// nothing after that moves it.
type scanner struct {
	f   string
	i   int
	arg int
	bad bool
}

func scanDirective(f string, i, arg int) (directive, bool) {
	s := &scanner{f: f, i: i, arg: arg}
	s.skip("#0+- ")
	s.index()
	s.widthOrPrecision()
	if s.at('.') {
		s.i++
		s.index()
		s.widthOrPrecision()
	}
	s.index()
	if s.bad || s.i >= len(f) {
		return directive{}, false
	}
	if f[s.i] == '%' {
		return directive{verb: '%', operand: -1, end: s.i, next: s.arg}, true
	}
	return directive{verb: f[s.i], operand: s.arg, end: s.i, next: s.arg + 1}, true
}

func (s *scanner) at(c byte) bool { return !s.bad && s.i < len(s.f) && s.f[s.i] == c }

func (s *scanner) skip(set string) {
	for s.i < len(s.f) && strings.IndexByte(set, s.f[s.i]) >= 0 {
		s.i++
	}
}

// widthOrPrecision skips a width or precision: `*` takes an operand, digits take none.
func (s *scanner) widthOrPrecision() {
	if !s.at('*') {
		s.skip("0123456789")
		return
	}
	if s.arg >= maxOperand {
		s.bad = true
		return
	}
	s.i++
	s.arg++
}

// index reads an optional `[n]` and moves the operand index to n-1. A malformed
// index, one below 1 and one above maxOperand make the format unreadable.
func (s *scanner) index() {
	if !s.at('[') {
		return
	}
	end := strings.IndexByte(s.f[s.i:], ']')
	if end < 0 {
		s.bad = true
		return
	}
	n, err := strconv.Atoi(s.f[s.i+1 : s.i+end])
	if err != nil || n < 1 || n > maxOperand {
		s.bad = true
		return
	}
	s.i += end + 1
	s.arg = n - 1
}
