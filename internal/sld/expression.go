package sld

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

func IsDynamicExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}")
}

func EvaluateNumericExpression(expression string, environment map[string]string) (float64, error) {
	value, err := evaluateExpression(expression, environment)
	if err != nil {
		return 0, err
	}
	number, err := value.numberValue()
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("expression result must be finite")
	}
	return number, nil
}

func EvaluateStringExpression(expression string, environment map[string]string) (string, error) {
	value, err := evaluateExpression(expression, environment)
	if err != nil {
		return "", err
	}
	if value.numeric {
		return strconv.FormatFloat(value.number, 'g', -1, 64), nil
	}
	return value.text, nil
}

type expressionValue struct {
	numeric bool
	number  float64
	text    string
}

func (v expressionValue) numberValue() (float64, error) {
	if v.numeric {
		return v.number, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(v.text), 64)
	if err != nil {
		return 0, fmt.Errorf("expression value %q is not numeric", v.text)
	}
	return parsed, nil
}

type expressionParser struct {
	input string
	pos   int
	env   map[string]string
}

func evaluateExpression(input string, environment map[string]string) (expressionValue, error) {
	input = strings.TrimSpace(input)
	if IsDynamicExpression(input) {
		input = strings.TrimSpace(input[2 : len(input)-1])
	}
	parser := &expressionParser{input: input, env: environment}
	value, err := parser.parseAdditive()
	if err != nil {
		return expressionValue{}, err
	}
	parser.skipSpace()
	if parser.pos != len(parser.input) {
		return expressionValue{}, fmt.Errorf("unexpected expression content at byte %d", parser.pos)
	}
	return value, nil
}

func (p *expressionParser) parseAdditive() (expressionValue, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return expressionValue{}, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) || (p.input[p.pos] != '+' && p.input[p.pos] != '-') {
			return left, nil
		}
		op := p.input[p.pos]
		p.pos++
		right, err := p.parseMultiplicative()
		if err != nil {
			return expressionValue{}, err
		}
		leftNumber, err := left.numberValue()
		if err != nil {
			return expressionValue{}, err
		}
		rightNumber, err := right.numberValue()
		if err != nil {
			return expressionValue{}, err
		}
		if op == '+' {
			left = expressionValue{numeric: true, number: leftNumber + rightNumber}
		} else {
			left = expressionValue{numeric: true, number: leftNumber - rightNumber}
		}
	}
}

func (p *expressionParser) parseMultiplicative() (expressionValue, error) {
	left, err := p.parseUnary()
	if err != nil {
		return expressionValue{}, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) || (p.input[p.pos] != '*' && p.input[p.pos] != '/') {
			return left, nil
		}
		op := p.input[p.pos]
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return expressionValue{}, err
		}
		leftNumber, err := left.numberValue()
		if err != nil {
			return expressionValue{}, err
		}
		rightNumber, err := right.numberValue()
		if err != nil {
			return expressionValue{}, err
		}
		if op == '*' {
			left = expressionValue{numeric: true, number: leftNumber * rightNumber}
		} else {
			if rightNumber == 0 {
				return expressionValue{}, fmt.Errorf("division by zero")
			}
			left = expressionValue{numeric: true, number: leftNumber / rightNumber}
		}
	}
}

func (p *expressionParser) parseUnary() (expressionValue, error) {
	p.skipSpace()
	if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
		op := p.input[p.pos]
		p.pos++
		value, err := p.parseUnary()
		if err != nil {
			return expressionValue{}, err
		}
		number, err := value.numberValue()
		if err != nil {
			return expressionValue{}, err
		}
		if op == '-' {
			number = -number
		}
		return expressionValue{numeric: true, number: number}, nil
	}
	return p.parsePrimary()
}

func (p *expressionParser) parsePrimary() (expressionValue, error) {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return expressionValue{}, fmt.Errorf("unexpected end of expression")
	}
	if p.input[p.pos] == '(' {
		p.pos++
		value, err := p.parseAdditive()
		if err != nil {
			return expressionValue{}, err
		}
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return expressionValue{}, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return value, nil
	}
	if p.input[p.pos] == '\'' || p.input[p.pos] == '"' {
		return p.parseString()
	}
	if unicode.IsLetter(rune(p.input[p.pos])) || p.input[p.pos] == '_' {
		name := p.parseIdentifier()
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] != '(' {
			return expressionValue{}, fmt.Errorf("unsupported expression function %q", name)
		}
		p.pos++
		var arguments []expressionValue
		p.skipSpace()
		if p.pos < len(p.input) && p.input[p.pos] != ')' {
			for {
				value, err := p.parseAdditive()
				if err != nil {
					return expressionValue{}, err
				}
				arguments = append(arguments, value)
				p.skipSpace()
				if p.pos < len(p.input) && p.input[p.pos] == ',' {
					p.pos++
					continue
				}
				break
			}
		}
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return expressionValue{}, fmt.Errorf("%s is missing a closing parenthesis", name)
		}
		p.pos++
		return evaluateFunction(strings.ToLower(name), arguments, p.env)
	}
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if !(c >= '0' && c <= '9') && c != '.' && c != 'e' && c != 'E' {
			if (c == '+' || c == '-') && p.pos > start && (p.input[p.pos-1] == 'e' || p.input[p.pos-1] == 'E') {
				p.pos++
				continue
			}
			break
		}
		p.pos++
	}
	if start == p.pos {
		return expressionValue{}, fmt.Errorf("unexpected expression character %q", p.input[p.pos])
	}
	value, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return expressionValue{}, fmt.Errorf("invalid number %q", p.input[start:p.pos])
	}
	return expressionValue{numeric: true, number: value}, nil
}

func evaluateFunction(name string, arguments []expressionValue, environment map[string]string) (expressionValue, error) {
	text := func(value expressionValue) string {
		if value.numeric {
			return strconv.FormatFloat(value.number, 'g', -1, 64)
		}
		return value.text
	}
	number := func(index int) (float64, error) {
		if index >= len(arguments) {
			return 0, fmt.Errorf("%s has too few arguments", name)
		}
		return arguments[index].numberValue()
	}
	switch name {
	case "env", "property":
		if len(arguments) < 1 {
			return expressionValue{}, fmt.Errorf("%s requires a key", name)
		}
		key := text(arguments[0])
		if value, ok := environment[key]; ok {
			return expressionValue{text: value}, nil
		}
		if len(arguments) > 1 {
			return arguments[1], nil
		}
		return expressionValue{text: ""}, nil
	case "strconcat", "concat":
		var result strings.Builder
		for _, argument := range arguments {
			result.WriteString(text(argument))
		}
		return expressionValue{text: result.String()}, nil
	case "lower":
		if len(arguments) != 1 {
			return expressionValue{}, fmt.Errorf("lower requires one argument")
		}
		return expressionValue{text: strings.ToLower(text(arguments[0]))}, nil
	case "upper":
		if len(arguments) != 1 {
			return expressionValue{}, fmt.Errorf("upper requires one argument")
		}
		return expressionValue{text: strings.ToUpper(text(arguments[0]))}, nil
	case "abs", "sqrt", "round", "floor", "ceil":
		value, err := number(0)
		if err != nil {
			return expressionValue{}, err
		}
		switch name {
		case "abs":
			value = math.Abs(value)
		case "sqrt":
			if value < 0 {
				return expressionValue{}, fmt.Errorf("sqrt requires a non-negative value")
			}
			value = math.Sqrt(value)
		case "round":
			value = math.Round(value)
		case "floor":
			value = math.Floor(value)
		case "ceil":
			value = math.Ceil(value)
		}
		return expressionValue{numeric: true, number: value}, nil
	case "pow":
		left, err := number(0)
		if err != nil {
			return expressionValue{}, err
		}
		right, err := number(1)
		if err != nil {
			return expressionValue{}, err
		}
		return expressionValue{numeric: true, number: math.Pow(left, right)}, nil
	case "min", "max":
		if len(arguments) == 0 {
			return expressionValue{}, fmt.Errorf("%s requires arguments", name)
		}
		result, err := number(0)
		if err != nil {
			return expressionValue{}, err
		}
		for index := 1; index < len(arguments); index++ {
			value, e := number(index)
			if e != nil {
				return expressionValue{}, e
			}
			if name == "min" {
				result = math.Min(result, value)
			} else {
				result = math.Max(result, value)
			}
		}
		return expressionValue{numeric: true, number: result}, nil
	default:
		return expressionValue{}, fmt.Errorf("unsupported expression function %q", name)
	}
}

func (p *expressionParser) parseString() (expressionValue, error) {
	quote := p.input[p.pos]
	p.pos++
	var out strings.Builder
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		p.pos++
		if c == quote {
			return expressionValue{text: out.String()}, nil
		}
		if c == '\\' && p.pos < len(p.input) {
			c = p.input[p.pos]
			p.pos++
		}
		out.WriteByte(c)
	}
	return expressionValue{}, fmt.Errorf("unterminated string literal")
}

func (p *expressionParser) parseIdentifier() string {
	start := p.pos
	for p.pos < len(p.input) {
		c := rune(p.input[p.pos])
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *expressionParser) skipSpace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}
