package linter_test

import (
	"strconv"
	"strings"

	. "github.com/onsi/gomega"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type fieldType = descriptorpb.FieldDescriptorProto_Type

const (
	tString  = descriptorpb.FieldDescriptorProto_TYPE_STRING
	tBytes   = descriptorpb.FieldDescriptorProto_TYPE_BYTES
	tBool    = descriptorpb.FieldDescriptorProto_TYPE_BOOL
	tEnum    = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	tMessage = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	tUint64  = descriptorpb.FieldDescriptorProto_TYPE_UINT64
	tInt32   = descriptorpb.FieldDescriptorProto_TYPE_INT32
)

// numericFamilies are the numeric `(buf.validate.field)` rule families, each
// with the scalar type its bounds and lists are declared with.
var numericFamilies = []struct {
	name string
	typ  fieldType
}{
	{"float", descriptorpb.FieldDescriptorProto_TYPE_FLOAT},
	{"double", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE},
	{"int32", descriptorpb.FieldDescriptorProto_TYPE_INT32},
	{"int64", descriptorpb.FieldDescriptorProto_TYPE_INT64},
	{"uint32", descriptorpb.FieldDescriptorProto_TYPE_UINT32},
	{"uint64", descriptorpb.FieldDescriptorProto_TYPE_UINT64},
	{"sint32", descriptorpb.FieldDescriptorProto_TYPE_SINT32},
	{"sint64", descriptorpb.FieldDescriptorProto_TYPE_SINT64},
	{"fixed32", descriptorpb.FieldDescriptorProto_TYPE_FIXED32},
	{"fixed64", descriptorpb.FieldDescriptorProto_TYPE_FIXED64},
	{"sfixed32", descriptorpb.FieldDescriptorProto_TYPE_SFIXED32},
	{"sfixed64", descriptorpb.FieldDescriptorProto_TYPE_SFIXED64},
}

func ruleField(name string, number int32, typ fieldType, typeName string, repeated bool) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}
	field := &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Label:  label.Enum(),
		Type:   typ.Enum(),
	}
	if typeName != "" {
		field.TypeName = proto.String(typeName)
	}
	return field
}

func enumValues(names ...string) []*descriptorpb.EnumValueDescriptorProto {
	values := make([]*descriptorpb.EnumValueDescriptorProto, 0, len(names))
	for _, spec := range names {
		name, number, _ := strings.Cut(spec, "=")
		n, err := strconv.Atoi(number)
		Expect(err).NotTo(HaveOccurred())
		values = append(values, &descriptorpb.EnumValueDescriptorProto{Name: proto.String(name), Number: proto.Int32(int32(n))})
	}
	return values
}

// validateProto is enough of protovalidate's validate.proto for the rules to
// resolve `(buf.validate.field)` by name: `required`, `ignore`, `field_mask`
// and a type family for every scalar kind, repeated fields and maps, at
// protovalidate's own field numbers. The rules read every option by name, so
// only the names have to be right. It is proto2, as validate.proto is, so a
// rule set to zero — `int32.gt = 0` — is still set. `ignore` also declares
// IGNORE_IF_DEFAULT_VALUE = 2, an older protovalidate's value.
func validateProto() *descriptorpb.FileDescriptorProto {
	messages := []*descriptorpb.DescriptorProto{
		{
			Name:  proto.String("FieldMaskRules"),
			Field: []*descriptorpb.FieldDescriptorProto{ruleField("in", 2, tString, "", true)},
		},
		{
			Name: proto.String("StringRules"),
			Field: []*descriptorpb.FieldDescriptorProto{
				ruleField("const", 1, tString, "", false),
				ruleField("len", 19, tUint64, "", false),
				ruleField("min_len", 2, tUint64, "", false),
				ruleField("max_len", 3, tUint64, "", false),
				ruleField("len_bytes", 20, tUint64, "", false),
				ruleField("min_bytes", 4, tUint64, "", false),
				ruleField("pattern", 6, tString, "", false),
				ruleField("prefix", 7, tString, "", false),
				ruleField("suffix", 8, tString, "", false),
				ruleField("contains", 9, tString, "", false),
				ruleField("not_contains", 23, tString, "", false),
				ruleField("in", 10, tString, "", true),
				ruleField("not_in", 11, tString, "", true),
				ruleField("email", 12, tBool, "", false),
				ruleField("hostname", 13, tBool, "", false),
				ruleField("uri_ref", 18, tBool, "", false),
				ruleField("uuid", 22, tBool, "", false),
				ruleField("well_known_regex", 24, tEnum, ".buf.validate.KnownRegex", false),
			},
		},
		{
			Name: proto.String("BytesRules"),
			Field: []*descriptorpb.FieldDescriptorProto{
				ruleField("const", 1, tBytes, "", false),
				ruleField("len", 13, tUint64, "", false),
				ruleField("min_len", 2, tUint64, "", false),
				ruleField("pattern", 4, tString, "", false),
				ruleField("prefix", 5, tBytes, "", false),
				ruleField("in", 8, tBytes, "", true),
				ruleField("not_in", 9, tBytes, "", true),
				ruleField("ip", 10, tBool, "", false),
			},
		},
		{
			Name:  proto.String("BoolRules"),
			Field: []*descriptorpb.FieldDescriptorProto{ruleField("const", 1, tBool, "", false)},
		},
		{
			Name: proto.String("EnumRules"),
			Field: []*descriptorpb.FieldDescriptorProto{
				ruleField("const", 1, tInt32, "", false),
				ruleField("defined_only", 2, tBool, "", false),
				ruleField("in", 3, tInt32, "", true),
				ruleField("not_in", 4, tInt32, "", true),
			},
		},
		{
			Name:  proto.String("RepeatedRules"),
			Field: []*descriptorpb.FieldDescriptorProto{ruleField("min_items", 1, tUint64, "", false)},
		},
		{
			Name:  proto.String("MapRules"),
			Field: []*descriptorpb.FieldDescriptorProto{ruleField("min_pairs", 1, tUint64, "", false)},
		},
	}
	fieldRules := []*descriptorpb.FieldDescriptorProto{
		ruleField("bool", 13, tMessage, ".buf.validate.BoolRules", false),
		ruleField("string", 14, tMessage, ".buf.validate.StringRules", false),
		ruleField("bytes", 15, tMessage, ".buf.validate.BytesRules", false),
		ruleField("enum", 16, tMessage, ".buf.validate.EnumRules", false),
		ruleField("repeated", 18, tMessage, ".buf.validate.RepeatedRules", false),
		ruleField("map", 19, tMessage, ".buf.validate.MapRules", false),
		ruleField("required", 25, tBool, "", false),
		ruleField("ignore", 27, tEnum, ".buf.validate.Ignore", false),
		ruleField("field_mask", 28, tMessage, ".buf.validate.FieldMaskRules", false),
	}
	for i, family := range numericFamilies {
		message := "Numeric" + family.name + "Rules"
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name: proto.String(message),
			Field: []*descriptorpb.FieldDescriptorProto{
				ruleField("const", 1, family.typ, "", false),
				ruleField("lt", 2, family.typ, "", false),
				ruleField("lte", 3, family.typ, "", false),
				ruleField("gt", 4, family.typ, "", false),
				ruleField("gte", 5, family.typ, "", false),
				ruleField("in", 6, family.typ, "", true),
				ruleField("not_in", 7, family.typ, "", true),
			},
		})
		fieldRules = append(fieldRules, ruleField(family.name, int32(i+1), tMessage, ".buf.validate."+message, false))
	}
	messages = append(messages, &descriptorpb.DescriptorProto{Name: proto.String("FieldRules"), Field: fieldRules})

	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String("buf/validate/validate.proto"),
		Package:     proto.String("buf.validate"),
		Syntax:      proto.String("proto2"),
		Dependency:  []string{"google/protobuf/descriptor.proto"},
		MessageType: messages,
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name:  proto.String("Ignore"),
				Value: enumValues("IGNORE_UNSPECIFIED=0", "IGNORE_IF_ZERO_VALUE=1", "IGNORE_IF_DEFAULT_VALUE=2", "IGNORE_ALWAYS=3"),
			},
			{
				Name:  proto.String("KnownRegex"),
				Value: enumValues("KNOWN_REGEX_UNSPECIFIED=0", "KNOWN_REGEX_HTTP_HEADER_NAME=1", "KNOWN_REGEX_HTTP_HEADER_VALUE=2"),
			},
		},
		Extension: []*descriptorpb.FieldDescriptorProto{{
			Name:     proto.String("field"),
			Number:   proto.Int32(1159),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".buf.validate.FieldRules"),
			Extendee: proto.String(".google.protobuf.FieldOptions"),
		}},
	}
}

// rules is `(buf.validate.field)` written as paths to values, e.g.
// {"string.min_len": uint64(1), "ignore": protoreflect.EnumNumber(1)}. A
// slice value appends to a repeated rule.
type rules map[string]any

// validated returns field options with behaviors and the given
// `(buf.validate.field)` rules, carried the way a plugin request carries
// them: the field behaviors resolved, protovalidate's rules as unknown fields.
func validated(validate protoreflect.FileDescriptor, set rules, behaviors ...annotations.FieldBehavior) *descriptorpb.FieldOptions {
	extension := validate.Extensions().ByName("field")
	root := dynamicpb.NewMessage(extension.Message())
	for path, value := range set {
		names := strings.Split(path, ".")
		message := protoreflect.Message(root)
		for _, name := range names[:len(names)-1] {
			field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
			Expect(field).NotTo(BeNil(), path)
			message = message.Mutable(field).Message()
		}
		field := message.Descriptor().Fields().ByName(protoreflect.Name(names[len(names)-1]))
		Expect(field).NotTo(BeNil(), path)
		if list, ok := value.([]any); ok {
			for _, item := range list {
				message.Mutable(field).List().Append(protoreflect.ValueOf(item))
			}
			continue
		}
		message.Set(field, protoreflect.ValueOf(value))
	}

	typed := behavior(behaviors...)
	proto.SetExtension(typed, dynamicpb.NewExtensionType(extension), root)
	raw, err := proto.Marshal(typed)
	Expect(err).NotTo(HaveOccurred())
	unknown := &descriptorpb.FieldOptions{}
	Expect(proto.Unmarshal(raw, unknown)).To(Succeed())
	return unknown
}

// shape is the declaration of the one field a zero-value test lints: its
// type, and whether it is `optional`, a oneof member, repeated or a map. An
// enum field is a `Color`, a message one a `Sub`.
type shape struct {
	typ      fieldType
	optional bool
	oneof    bool
	repeated bool
	isMap    bool
}

// declare adds a field named value of shape s to message, with options,
// declaring whatever it needs: a oneof, the synthetic oneof of an `optional`
// field, a map entry.
func declare(message *descriptorpb.DescriptorProto, s shape, options *descriptorpb.FieldOptions) {
	field := ruleField("value", 2, s.typ, "", s.repeated || s.isMap)
	field.Options = options
	switch s.typ {
	case tEnum:
		field.TypeName = proto.String(".example.v1.Color")
	case tMessage:
		field.TypeName = proto.String(".example.v1.Sub")
	}
	if s.isMap {
		field.Type = tMessage.Enum()
		field.TypeName = proto.String(".example.v1." + message.GetName() + ".ValueEntry")
		message.NestedType = append(message.NestedType, &descriptorpb.DescriptorProto{
			Name:    proto.String("ValueEntry"),
			Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
			Field: []*descriptorpb.FieldDescriptorProto{
				ruleField("key", 1, tString, "", false),
				ruleField("value", 2, tString, "", false),
			},
		})
	}
	if s.oneof || s.optional {
		name := "choice"
		if s.optional {
			name = "_value"
			field.Proto3Optional = proto.Bool(true)
		}
		field.OneofIndex = proto.Int32(int32(len(message.OneofDecl)))
		message.OneofDecl = append(message.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: proto.String(name)})
	}
	message.Field = append(message.Field, field)
}

// supporting is the `Color` enum and `Sub` message a shape may refer to.
func supporting() ([]*descriptorpb.EnumDescriptorProto, *descriptorpb.DescriptorProto) {
	color := &descriptorpb.EnumDescriptorProto{
		Name:  proto.String("Color"),
		Value: enumValues("COLOR_UNSPECIFIED=0", "COLOR_RED=1", "COLOR_BLUE=2"),
	}
	sub := &descriptorpb.DescriptorProto{
		Name:  proto.String("Sub"),
		Field: []*descriptorpb.FieldDescriptorProto{ruleField("note", 1, tString, "", false)},
	}
	return []*descriptorpb.EnumDescriptorProto{color}, sub
}
