package linter

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// zeroRejection names the `(buf.validate.field)` rule on field that
// protovalidate fails when a request leaves field unset — the zero value, for
// a field without presence — or returns "" when no rule does, or none can be
// decided exactly. required reports whether that rule is `required`.
//
// The order is protovalidate's own: IGNORE_ALWAYS skips every rule; then
// `required` fails an unset field, whatever its presence and its other
// `ignore`; then an unset field with presence, or one under
// IGNORE_IF_ZERO_VALUE, is skipped; and only then do the type's rules see the
// zero value. Those are decided only where the answer is exact: a pattern is
// compiled as RE2 and tried on "", a bound is compared with zero the way
// protovalidate combines it with its counterpart, and `cel` rules are never
// evaluated.
func zeroRejection(field protoreflect.FieldDescriptor) (rule string, required bool) {
	value, ok := fieldOption(field, validateFieldExtension)
	if !ok {
		return "", false
	}
	rules := value.Message()
	ignore := ignoreOf(rules)
	if ignore == ignoreAlways || ignore == ignoreUnknown {
		return "", false
	}
	if set, ok := scalar(rules, "required"); ok && set.Bool() {
		return "`required = true`", true
	}
	if field.HasPresence() || ignore == ignoreZero {
		return "", false
	}
	return typeRejection(field, rules), false
}

type ignoreMode int

const (
	ignoreNone ignoreMode = iota
	ignoreZero
	ignoreAlways
	ignoreUnknown
)

// ignoreOf reads `ignore` by the names its value is declared with, so the
// aliases older protovalidate versions declare — IGNORE_IF_UNPOPULATED,
// IGNORE_EMPTY, IGNORE_IF_DEFAULT_VALUE, IGNORE_DEFAULT, each of which skips a
// proto3 field's zero value — are read as what they mean. A value the schema's
// validate.proto does not declare cannot be decided.
func ignoreOf(rules protoreflect.Message) ignoreMode {
	field := rules.Descriptor().Fields().ByName("ignore")
	if field == nil || field.Enum() == nil || !rules.Has(field) {
		return ignoreNone
	}
	number := rules.Get(field).Enum()
	values := field.Enum().Values()
	mode := ignoreUnknown
	for i := range values.Len() {
		value := values.Get(i)
		if value.Number() != number {
			continue
		}
		switch value.Name() {
		case "IGNORE_UNSPECIFIED":
			mode = ignoreNone
		case "IGNORE_IF_ZERO_VALUE", "IGNORE_IF_UNPOPULATED", "IGNORE_EMPTY", "IGNORE_IF_DEFAULT_VALUE", "IGNORE_DEFAULT":
			mode = ignoreZero
		case "IGNORE_ALWAYS":
			return ignoreAlways
		}
	}
	return mode
}

// typeRejection names the rule among field's type rules — `string`, `int32`,
// `repeated` and the rest, whichever matches field — that fails its zero
// value, or returns "".
func typeRejection(field protoreflect.FieldDescriptor, rules protoreflect.Message) string {
	family := ruleFamily(field)
	if family == "" {
		return ""
	}
	typed, ok := scalar(rules, protoreflect.Name(family))
	if !ok {
		return ""
	}
	m := typed.Message()
	switch family {
	case "repeated":
		return atLeastOne(m, family, "min_items")
	case "map":
		return atLeastOne(m, family, "min_pairs")
	case "string":
		return stringRejection(m)
	case "bytes":
		return bytesRejection(m)
	case "bool":
		if value, ok := scalar(m, "const"); ok && value.Bool() {
			return "`bool.const = true`"
		}
		return ""
	case "enum":
		return enumRejection(m)
	default:
		return numericRejection(m, family, field.Kind())
	}
}

// ruleFamily is the `(buf.validate.field)` oneof member holding field's type
// rules, or "" for a message, whose zero value is its absence.
func ruleFamily(field protoreflect.FieldDescriptor) string {
	switch {
	case field.IsMap():
		return "map"
	case field.IsList():
		return "repeated"
	}
	switch field.Kind() {
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.EnumKind:
		return "enum"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind:
		return field.Kind().String()
	}
	return ""
}

// stringFormats are the `string` well-known formats that fail "".
// `uri_ref` is not one: "" is a relative reference.
var stringFormats = []protoreflect.Name{
	"email", "hostname", "ip", "ipv4", "ipv6", "uri", "address", "uuid", "tuuid",
	"ip_with_prefixlen", "ipv4_with_prefixlen", "ipv6_with_prefixlen",
	"ip_prefix", "ipv4_prefix", "ipv6_prefix", "host_and_port", "ulid",
	"protobuf_fqn", "protobuf_dot_fqn",
}

// bytesFormats are the `bytes` well-known formats that fail empty bytes.
var bytesFormats = []protoreflect.Name{"ip", "ipv4", "ipv6", "uuid"}

func stringRejection(m protoreflect.Message) string {
	if rule := emptyRejection(m, "string", func(v protoreflect.Value) string { return v.String() }); rule != "" {
		return rule
	}
	for _, name := range []protoreflect.Name{"len_bytes", "min_bytes"} {
		if rule := atLeastOne(m, "string", name); rule != "" {
			return rule
		}
	}
	if value, ok := scalar(m, "not_contains"); ok && value.String() == "" {
		return "`string.not_contains = \"\"`"
	}
	if rule := formatRejection(m, "string", stringFormats); rule != "" {
		return rule
	}
	if value, ok := scalar(m, "well_known_regex"); ok {
		field := m.Descriptor().Fields().ByName("well_known_regex")
		if name := field.Enum().Values().ByNumber(value.Enum()); name != nil && name.Name() == "KNOWN_REGEX_HTTP_HEADER_NAME" {
			return "`string.well_known_regex = KNOWN_REGEX_HTTP_HEADER_NAME`"
		}
	}
	return ""
}

func bytesRejection(m protoreflect.Message) string {
	if rule := emptyRejection(m, "bytes", func(v protoreflect.Value) string { return string(v.Bytes()) }); rule != "" {
		return rule
	}
	return formatRejection(m, "bytes", bytesFormats)
}

// emptyRejection checks the rules `string` and `bytes` share, reading each
// value as a string: `const`, `len`, `min_len`, `pattern`, `prefix`,
// `suffix`, `contains`, `in` and `not_in`.
func emptyRejection(m protoreflect.Message, family string, text func(protoreflect.Value) string) string {
	if value, ok := scalar(m, "const"); ok && text(value) != "" {
		return fmt.Sprintf("`%s.const = %q`", family, text(value))
	}
	for _, name := range []protoreflect.Name{"len", "min_len"} {
		if rule := atLeastOne(m, family, name); rule != "" {
			return rule
		}
	}
	if value, ok := scalar(m, "pattern"); ok {
		if pattern, err := regexp.Compile(value.String()); err == nil && !pattern.MatchString("") {
			return fmt.Sprintf("`%s.pattern = %q`", family, value.String())
		}
	}
	for _, name := range []protoreflect.Name{"prefix", "suffix", "contains"} {
		if value, ok := scalar(m, name); ok && text(value) != "" {
			return fmt.Sprintf("`%s.%s = %q`", family, name, text(value))
		}
	}
	in, notIn := listOf(m, "in", text), listOf(m, "not_in", text)
	if len(in) > 0 && !slices.Contains(in, "") {
		return fmt.Sprintf("`%s.in = %s`", family, quoted(in))
	}
	if slices.Contains(notIn, "") {
		return fmt.Sprintf("`%s.not_in = %s`", family, quoted(notIn))
	}
	return ""
}

func formatRejection(m protoreflect.Message, family string, formats []protoreflect.Name) string {
	for _, name := range formats {
		if value, ok := scalar(m, name); ok && value.Bool() {
			return fmt.Sprintf("`%s.%s = true`", family, name)
		}
	}
	return ""
}

func enumRejection(m protoreflect.Message) string {
	if value, ok := scalar(m, "const"); ok && value.Int() != 0 {
		return fmt.Sprintf("`enum.const = %d`", value.Int())
	}
	number := func(v protoreflect.Value) int64 { return v.Int() }
	in, notIn := listOf(m, "in", number), listOf(m, "not_in", number)
	if len(in) > 0 && !slices.Contains(in, 0) {
		return fmt.Sprintf("`enum.in = %v`", in)
	}
	if slices.Contains(notIn, 0) {
		return fmt.Sprintf("`enum.not_in = %v`", notIn)
	}
	return ""
}

// numericRejection checks a numeric family's `const`, `in`, `not_in` and
// bounds against zero.
//
// A lower bound (`gt`, `gte`) and an upper one (`lt`, `lte`) combine as
// protovalidate combines them: when the upper is at least the lower, a value
// must satisfy both; when it is below, either — the range is exclusive.
func numericRejection(m protoreflect.Message, family string, kind protoreflect.Kind) string {
	sign := func(v protoreflect.Value) (int, bool) { return signOf(v, kind) }
	format := func(name protoreflect.Name, v protoreflect.Value) string {
		return fmt.Sprintf("`%s.%s = %v`", family, name, v.Interface())
	}
	if value, ok := scalar(m, "const"); ok {
		if s, ok := sign(value); ok && s != 0 {
			return format("const", value)
		}
	}
	isZero := func(v protoreflect.Value) bool { s, ok := sign(v); return ok && s == 0 }
	if in, ok := scalar(m, "in"); ok && !anyOf(in.List(), isZero) {
		return fmt.Sprintf("`%s.in = %v`", family, values(in.List()))
	}
	if notIn, ok := scalar(m, "not_in"); ok && anyOf(notIn.List(), isZero) {
		return fmt.Sprintf("`%s.not_in = %v`", family, values(notIn.List()))
	}

	lower, lowerName, hasLower := bound(m, "gt", "gte")
	upper, upperName, hasUpper := bound(m, "lt", "lte")
	if !hasLower && !hasUpper {
		return ""
	}
	// Whether zero passes each bound: 0 > gt, 0 >= gte, 0 < lt, 0 <= lte.
	passes := func(v protoreflect.Value, name protoreflect.Name) (bool, bool) {
		s, ok := sign(v)
		if !ok {
			return false, false
		}
		switch name {
		case "gt":
			return s < 0, true
		case "gte":
			return s <= 0, true
		case "lt":
			return s > 0, true
		default:
			return s >= 0, true
		}
	}
	switch {
	case hasLower && hasUpper:
		passLower, ok := passes(lower, lowerName)
		if !ok {
			return ""
		}
		passUpper, ok := passes(upper, upperName)
		if !ok {
			return ""
		}
		order, ok := compare(upper, lower, kind)
		if !ok {
			return ""
		}
		pass := passLower || passUpper
		if order >= 0 {
			pass = passLower && passUpper
		}
		if !pass {
			return format(lowerName, lower) + " with " + format(upperName, upper)
		}
		return ""
	case hasLower:
		if pass, ok := passes(lower, lowerName); ok && !pass {
			return format(lowerName, lower)
		}
	default:
		if pass, ok := passes(upper, upperName); ok && !pass {
			return format(upperName, upper)
		}
	}
	return ""
}

// bound returns whichever of the two oneof members a or b is set.
func bound(m protoreflect.Message, a, b protoreflect.Name) (protoreflect.Value, protoreflect.Name, bool) {
	for _, name := range []protoreflect.Name{a, b} {
		if value, ok := scalar(m, name); ok {
			return value, name, true
		}
	}
	return protoreflect.Value{}, "", false
}

// signOf compares a numeric value with zero, false for NaN.
func signOf(v protoreflect.Value, kind protoreflect.Kind) (int, bool) {
	switch kind {
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return cmp.Compare(v.Uint(), 0), true
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		if math.IsNaN(v.Float()) {
			return 0, false
		}
		return cmp.Compare(v.Float(), 0), true
	default:
		return cmp.Compare(v.Int(), 0), true
	}
}

// compare orders two numeric values of kind exactly, false for NaN.
func compare(a, b protoreflect.Value, kind protoreflect.Kind) (int, bool) {
	switch kind {
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return cmp.Compare(a.Uint(), b.Uint()), true
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		if math.IsNaN(a.Float()) || math.IsNaN(b.Float()) {
			return 0, false
		}
		return cmp.Compare(a.Float(), b.Float()), true
	default:
		return cmp.Compare(a.Int(), b.Int()), true
	}
}

// atLeastOne names rule `family.name` when it is set to at least 1.
func atLeastOne(m protoreflect.Message, family string, name protoreflect.Name) string {
	if value, ok := scalar(m, name); ok && value.Uint() >= 1 {
		return fmt.Sprintf("`%s.%s = %d`", family, name, value.Uint())
	}
	return ""
}

// scalar returns the value of m's field name when it is declared and set.
func scalar(m protoreflect.Message, name protoreflect.Name) (protoreflect.Value, bool) {
	field := m.Descriptor().Fields().ByName(name)
	if field == nil || !m.Has(field) {
		return protoreflect.Value{}, false
	}
	return m.Get(field), true
}

func listOf[T any](m protoreflect.Message, name protoreflect.Name, convert func(protoreflect.Value) T) []T {
	value, ok := scalar(m, name)
	if !ok {
		return nil
	}
	list := value.List()
	out := make([]T, 0, list.Len())
	for i := range list.Len() {
		out = append(out, convert(list.Get(i)))
	}
	return out
}

func anyOf(list protoreflect.List, match func(protoreflect.Value) bool) bool {
	for i := range list.Len() {
		if match(list.Get(i)) {
			return true
		}
	}
	return false
}

func values(list protoreflect.List) []any {
	out := make([]any, 0, list.Len())
	for i := range list.Len() {
		out = append(out, list.Get(i).Interface())
	}
	return out
}

func quoted(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = fmt.Sprintf("%q", item)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
