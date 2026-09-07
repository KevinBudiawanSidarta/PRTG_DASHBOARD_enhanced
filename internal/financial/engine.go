package financial

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/shopspring/decimal"
)

type InputSnapshot struct {
	DurationSeconds        int64
	HourlyRevenue          decimal.Decimal
	TransactionsPerHour    decimal.Decimal
	AvgTransactionValue    decimal.Decimal
	ServiceDependency      decimal.Decimal
	LossProbability        decimal.Decimal
	OperationalCostPerHour decimal.Decimal
	PenaltyExposure        decimal.Decimal
	RecoveryCost           decimal.Decimal
}

type Component struct{ Key, Expr string }
type Model struct {
	Version    string
	Components []Component
	TotalExpr  string
}
type Result struct {
	Breakdown map[string]decimal.Decimal
	Total     decimal.Decimal
}

func vars(in InputSnapshot) map[string]float64 {
	return map[string]float64{
		"duration_seconds":          float64(in.DurationSeconds),
		"hourly_revenue":            in.HourlyRevenue.InexactFloat64(),
		"transactions_per_hour":     in.TransactionsPerHour.InexactFloat64(),
		"avg_transaction_value":     in.AvgTransactionValue.InexactFloat64(),
		"service_dependency":        in.ServiceDependency.InexactFloat64(),
		"loss_probability":          in.LossProbability.InexactFloat64(),
		"operational_cost_per_hour": in.OperationalCostPerHour.InexactFloat64(),
		"penalty_exposure":          in.PenaltyExposure.InexactFloat64(),
		"recovery_cost":             in.RecoveryCost.InexactFloat64(),
	}
}

func Calculate(model Model, in InputSnapshot) (Result, error) {
	v := vars(in)
	out := Result{Breakdown: map[string]decimal.Decimal{}}
	for _, c := range model.Components {
		f, err := evaluate(c.Expr, v)
		if err != nil {
			return Result{}, fmt.Errorf("component %s: %w", c.Key, err)
		}
		x, err := decimal.NewFromString(strconv.FormatFloat(f, 'f', 6, 64))
		if err != nil {
			return Result{}, err
		}
		out.Breakdown[c.Key] = x.Round(2)
	}
	f, err := evaluate(model.TotalExpr, mapStringDecimalToFloat(out.Breakdown))
	if err != nil {
		return Result{}, fmt.Errorf("total: %w", err)
	}
	out.Total = decimal.NewFromFloat(f).Round(2)
	return out, nil
}

func mapStringDecimalToFloat(m map[string]decimal.Decimal) map[string]float64 {
	r := map[string]float64{}
	for k, v := range m {
		r[k] = v.InexactFloat64()
	}
	return r
}

type parser struct {
	s    string
	pos  int
	vars map[string]float64
}

func evaluate(s string, v map[string]float64) (float64, error) {
	p := &parser{s: s, vars: v}
	r, err := p.expr()
	if err != nil {
		return 0, err
	}
	p.skip()
	if p.pos != len(p.s) {
		return 0, fmt.Errorf("unexpected token at %d", p.pos)
	}
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0, fmt.Errorf("non-finite result")
	}
	return r, nil
}
func (p *parser) skip() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}
func (p *parser) eat(c byte) bool {
	p.skip()
	if p.pos < len(p.s) && p.s[p.pos] == c {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expr() (float64, error) {
	a, e := p.term()
	if e != nil {
		return 0, e
	}
	for {
		if p.eat('+') {
			b, e := p.term()
			if e != nil {
				return 0, e
			}
			a += b
		} else if p.eat('-') {
			b, e := p.term()
			if e != nil {
				return 0, e
			}
			a -= b
		} else {
			return a, nil
		}
	}
}
func (p *parser) term() (float64, error) {
	a, e := p.factor()
	if e != nil {
		return 0, e
	}
	for {
		if p.eat('*') {
			b, e := p.factor()
			if e != nil {
				return 0, e
			}
			a *= b
		} else if p.eat('/') {
			b, e := p.factor()
			if e != nil {
				return 0, e
			}
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			a /= b
		} else {
			return a, nil
		}
	}
}
func (p *parser) factor() (float64, error) {
	p.skip()
	if p.eat('(') {
		a, e := p.expr()
		if e != nil {
			return 0, e
		}
		if !p.eat(')') {
			return 0, fmt.Errorf("missing )")
		}
		return a, nil
	}
	if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
		neg := p.s[p.pos] == '-'
		p.pos++
		a, e := p.factor()
		if e != nil {
			return 0, e
		}
		if neg {
			a = -a
		}
		return a, nil
	}
	start := p.pos
	for p.pos < len(p.s) && (unicode.IsLetter(rune(p.s[p.pos])) || unicode.IsDigit(rune(p.s[p.pos])) || p.s[p.pos] == '_') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected value at %d", p.pos)
	}
	tok := strings.TrimSpace(p.s[start:p.pos])
	if x, err := strconv.ParseFloat(tok, 64); err == nil {
		return x, nil
	}
	v, ok := p.vars[tok]
	if !ok {
		return 0, fmt.Errorf("unknown variable %q", tok)
	}
	return v, nil
}
