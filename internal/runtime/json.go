package runtime

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// JsonValue is a simple recursive JSON value type used for lightweight
// parsing and rendering without pulling in encoding/json.
type JsonValue struct {
	typ    jsonType
	boolV  bool
	numV   int64
	strV   string
	arrV   []JsonValue
	objV   map[string]JsonValue // sorted by key on render
}

type jsonType int

const (
	jsonNull  jsonType = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

// JsonNull is the null JSON value.
var JsonNull = JsonValue{typ: jsonNull}

// JsonBool creates a boolean JSON value.
func JsonBoolVal(v bool) JsonValue { return JsonValue{typ: jsonBool, boolV: v} }

// JsonNumber creates a numeric JSON value.
func JsonNumberVal(v int64) JsonValue { return JsonValue{typ: jsonNumber, numV: v} }

// JsonStringVal creates a string JSON value.
func JsonStringVal(v string) JsonValue { return JsonValue{typ: jsonString, strV: v} }

// JsonArrayVal creates an array JSON value.
func JsonArrayVal(v []JsonValue) JsonValue { return JsonValue{typ: jsonArray, arrV: v} }

// JsonObjectVal creates an object JSON value.
func JsonObjectVal(v map[string]JsonValue) JsonValue { return JsonValue{typ: jsonObject, objV: v} }

// Render serializes the JsonValue back to a JSON string.
func (v JsonValue) Render() string {
	switch v.typ {
	case jsonNull:
		return "null"
	case jsonBool:
		if v.boolV {
			return "true"
		}
		return "false"
	case jsonNumber:
		return fmt.Sprintf("%d", v.numV)
	case jsonString:
		return renderJsonString(v.strV)
	case jsonArray:
		parts := make([]string, len(v.arrV))
		for i, elem := range v.arrV {
			parts[i] = elem.Render()
		}
		return "[" + strings.Join(parts, ",") + "]"
	case jsonObject:
		keys := make([]string, 0, len(v.objV))
		for k := range v.objV {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = renderJsonString(k) + ":" + v.objV[k].Render()
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		return "null"
	}
}

// ParseJson parses a JSON string into a JsonValue.
func ParseJson(source string) (JsonValue, error) {
	p := newJsonParser(source)
	val, err := p.parseValue()
	if err != nil {
		return JsonNull, err
	}
	p.skipWhitespace()
	if !p.isEOF() {
		return JsonNull, fmt.Errorf("unexpected trailing content at position %d", p.pos)
	}
	return val, nil
}

// AsObject returns the underlying map if this is an object, else nil.
func (v JsonValue) AsObject() map[string]JsonValue {
	if v.typ == jsonObject {
		return v.objV
	}
	return nil
}

// AsArray returns the underlying slice if this is an array, else nil.
func (v JsonValue) AsArray() []JsonValue {
	if v.typ == jsonArray {
		return v.arrV
	}
	return nil
}

// AsString returns the string value if this is a string, else empty.
func (v JsonValue) AsString() (string, bool) {
	if v.typ == jsonString {
		return v.strV, true
	}
	return "", false
}

// AsBool returns the bool value if this is a bool, else false.
func (v JsonValue) AsBool() (bool, bool) {
	if v.typ == jsonBool {
		return v.boolV, true
	}
	return false, false
}

// AsInt64 returns the int64 value if this is a number, else 0.
func (v JsonValue) AsInt64() (int64, bool) {
	if v.typ == jsonNumber {
		return v.numV, true
	}
	return 0, false
}

// renderJsonString escapes and quotes a string for JSON output.
func renderJsonString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, ch := range s {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if unicode.IsControl(ch) {
				b.WriteString(fmt.Sprintf(`\u%04x`, ch))
			} else {
				b.WriteRune(ch)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsonParser holds state for incremental JSON parsing.
type jsonParser struct {
	chars []rune
	pos   int
}

func newJsonParser(source string) *jsonParser {
	return &jsonParser{
		chars: []rune(source),
		pos:   0,
	}
}

func (p *jsonParser) parseValue() (JsonValue, error) {
	p.skipWhitespace()
	if p.isEOF() {
		return JsonNull, fmt.Errorf("unexpected end of input")
	}
	switch p.peek() {
	case 'n':
		return p.parseLiteral("null", JsonNull)
	case 't':
		return p.parseLiteral("true", JsonBoolVal(true))
	case 'f':
		return p.parseLiteral("false", JsonBoolVal(false))
	case '"':
		s, err := p.parseString()
		if err != nil {
			return JsonNull, err
		}
		return JsonStringVal(s), nil
	case '[':
		return p.parseArray()
	case '{':
		return p.parseObject()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		n, err := p.parseNumber()
		if err != nil {
			return JsonNull, err
		}
		return JsonNumberVal(n), nil
	default:
		return JsonNull, fmt.Errorf("unexpected character: %c", p.peek())
	}
}

func (p *jsonParser) parseLiteral(expected string, value JsonValue) (JsonValue, error) {
	for _, ch := range expected {
		if p.next() != ch {
			return JsonNull, fmt.Errorf("invalid literal: expected %s", expected)
		}
	}
	return value, nil
}

func (p *jsonParser) parseString() (string, error) {
	if err := p.expect('"'); err != nil {
		return "", err
	}
	var b strings.Builder
	for {
		ch, ok := p.nextRune()
		if !ok {
			return "", fmt.Errorf("unterminated string")
		}
		if ch == '"' {
			return b.String(), nil
		}
		if ch == '\\' {
			escaped, err := p.parseEscape()
			if err != nil {
				return "", err
			}
			b.WriteRune(escaped)
		} else {
			b.WriteRune(ch)
		}
	}
}

func (p *jsonParser) parseEscape() (rune, error) {
	ch, ok := p.nextRune()
	if !ok {
		return 0, fmt.Errorf("unexpected end of input in escape sequence")
	}
	switch ch {
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case '/':
		return '/', nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		return p.parseUnicodeEscape()
	default:
		return 0, fmt.Errorf("invalid escape sequence: %c", ch)
	}
}

func (p *jsonParser) parseUnicodeEscape() (rune, error) {
	var val uint32
	for i := 0; i < 4; i++ {
		ch, ok := p.nextRune()
		if !ok {
			return 0, fmt.Errorf("unexpected end of input in unicode escape")
		}
		digit, ok := hexDigit(ch)
		if !ok {
			return 0, fmt.Errorf("invalid unicode escape hex digit: %c", ch)
		}
		val = (val << 4) | digit
	}
	r := rune(val)
	if !utf8.ValidRune(r) {
		return 0, fmt.Errorf("invalid unicode scalar value: %04x", val)
	}
	return r, nil
}

func hexDigit(ch rune) (uint32, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return uint32(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return uint32(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return uint32(ch-'A') + 10, true
	default:
		return 0, false
	}
}

func (p *jsonParser) parseArray() (JsonValue, error) {
	if err := p.expect('['); err != nil {
		return JsonNull, err
	}
	var values []JsonValue
	for {
		p.skipWhitespace()
		if p.tryConsume(']') {
			break
		}
		v, err := p.parseValue()
		if err != nil {
			return JsonNull, err
		}
		values = append(values, v)
		p.skipWhitespace()
		if p.tryConsume(']') {
			break
		}
		if err := p.expect(','); err != nil {
			return JsonNull, err
		}
	}
	return JsonArrayVal(values), nil
}

func (p *jsonParser) parseObject() (JsonValue, error) {
	if err := p.expect('{'); err != nil {
		return JsonNull, err
	}
	entries := make(map[string]JsonValue)
	for {
		p.skipWhitespace()
		if p.tryConsume('}') {
			break
		}
		key, err := p.parseString()
		if err != nil {
			return JsonNull, err
		}
		p.skipWhitespace()
		if err := p.expect(':'); err != nil {
			return JsonNull, err
		}
		val, err := p.parseValue()
		if err != nil {
			return JsonNull, err
		}
		entries[key] = val
		p.skipWhitespace()
		if p.tryConsume('}') {
			break
		}
		if err := p.expect(','); err != nil {
			return JsonNull, err
		}
	}
	return JsonObjectVal(entries), nil
}

func (p *jsonParser) parseNumber() (int64, error) {
	var b strings.Builder
	if p.peek() == '-' {
		b.WriteRune(p.next())
	}
	for !p.isEOF() {
		ch := p.peek()
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
			p.pos++
		} else {
			break
		}
	}
	s := b.String()
	if s == "" || s == "-" {
		return 0, fmt.Errorf("invalid number")
	}
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("number out of range: %s", s)
	}
	return n, nil
}

func (p *jsonParser) expect(ch rune) error {
	actual, ok := p.nextRune()
	if !ok {
		return fmt.Errorf("expected '%c', found end of input", ch)
	}
	if actual != ch {
		return fmt.Errorf("expected '%c', found '%c'", ch, actual)
	}
	return nil
}

func (p *jsonParser) tryConsume(ch rune) bool {
	if !p.isEOF() && p.peek() == ch {
		p.pos++
		return true
	}
	return false
}

func (p *jsonParser) skipWhitespace() {
	for !p.isEOF() {
		ch := p.peek()
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
			p.pos++
		} else {
			break
		}
	}
}

func (p *jsonParser) peek() rune {
	if p.isEOF() {
		return 0
	}
	return p.chars[p.pos]
}

func (p *jsonParser) next() rune {
	if p.isEOF() {
		return 0
	}
	ch := p.chars[p.pos]
	p.pos++
	return ch
}

func (p *jsonParser) nextRune() (rune, bool) {
	if p.isEOF() {
		return 0, false
	}
	ch := p.chars[p.pos]
	p.pos++
	return ch, true
}

func (p *jsonParser) isEOF() bool {
	return p.pos >= len(p.chars)
}
