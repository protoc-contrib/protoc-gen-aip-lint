package linter_test

import (
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/protoc-contrib/protoc-gen-aip-lint/internal/linter"
)

const (
	REQUIRED   = annotations.FieldBehavior_REQUIRED
	OPTIONAL   = annotations.FieldBehavior_OPTIONAL
	IDENTIFIER = annotations.FieldBehavior_IDENTIFIER
)

var (
	ignoreZero    = protoreflect.EnumNumber(1)
	ignoreDefault = protoreflect.EnumNumber(2)
	ignoreAlways  = protoreflect.EnumNumber(3)
)

func resourceOptions(kind, pattern string) *descriptorpb.MessageOptions {
	options := &descriptorpb.MessageOptions{}
	proto.SetExtension(options, annotations.E_Resource, &annotations.ResourceDescriptor{
		Type:    "example.com/" + kind,
		Pattern: []string{pattern},
	})
	return options
}

// schemaFiles is a registry holding what every test schema imports, and the
// validate.proto fixture in it.
func schemaFiles() (*protoregistry.Files, protoreflect.FileDescriptor) {
	files := new(protoregistry.Files)
	for _, dependency := range []protoreflect.FileDescriptor{
		descriptorpb.File_google_protobuf_descriptor_proto,
		annotations.File_google_api_field_behavior_proto,
		annotations.File_google_api_resource_proto,
	} {
		Expect(files.RegisterFile(dependency)).To(Succeed())
	}
	validate, err := protodesc.NewFile(validateProto(), files)
	Expect(err).NotTo(HaveOccurred())
	Expect(files.RegisterFile(validate)).To(Succeed())
	return files, validate
}

// problemsOf lints files and returns what rule reports, keyed by the full
// name of the descriptor each problem is reported on.
func problemsOf(rule protoreflect.FullName, files ...protoreflect.FileDescriptor) map[protoreflect.FullName][]string {
	l, err := linter.New(&linter.Config{})
	Expect(err).NotTo(HaveOccurred())
	responses, err := l.LintProtos(files...)
	Expect(err).NotTo(HaveOccurred())

	problems := map[protoreflect.FullName][]string{}
	for _, response := range responses {
		for _, problem := range response.Problems {
			if protoreflect.FullName(problem.RuleID) == rule {
				name := problem.Descriptor.FullName()
				problems[name] = append(problems[name], problem.Message)
			}
		}
	}
	return problems
}

// lintResourceValue lints a `Thing` resource whose only field besides `name`
// is `value`, REQUIRED, of shape s, with the given rules, and returns what
// ResourceRequiredRejectsZero reports on it.
func lintResourceValue(s shape, set rules) []string {
	files, validate := schemaFiles()
	enums, sub := supporting()
	thing := &descriptorpb.DescriptorProto{
		Name:    proto.String("Thing"),
		Options: resourceOptions("Thing", "things/{thing}"),
		Field:   []*descriptorpb.FieldDescriptorProto{stringField("name", 1, behavior(IDENTIFIER))},
	}
	options := behavior(REQUIRED)
	if set != nil {
		options = validated(validate, set, REQUIRED)
	}
	declare(thing, s, options)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        proto.String("example/v1/thing.proto"),
		Package:     proto.String("example.v1"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"google/api/field_behavior.proto", "google/api/resource.proto", "buf/validate/validate.proto"},
		MessageType: []*descriptorpb.DescriptorProto{sub, thing},
		EnumType:    enums,
	}, files)
	Expect(err).NotTo(HaveOccurred())
	return problemsOf(protoreflect.FullName(linter.ResourceRequiredRejectsZero), file)["example.v1.Thing.value"]
}

// lintResource builds a `Book` resource and returns the problems
// ResourceRequiredRejectsZero reports for it. `Book` holds:
//
//   - `title`: REQUIRED with `ignore` + `min_len` (allowed);
//   - `isbn`: REQUIRED with `required` (reported);
//   - `summary`: OPTIONAL with `required` (not REQUIRED, so not this rule's);
//   - `address`: a singular `Address` whose `city` is REQUIRED with `required`
//     (reported) and whose `street` is REQUIRED alone (allowed);
//   - `edition`: a singular `Book.Edition`, declared inside `Book`, whose
//     `number` is REQUIRED with `required` (reported);
//   - `chapters`: repeated `Chapter`s whose `title` is REQUIRED with
//     `required` (allowed: an element is always written whole);
//   - `location`: a singular `Location` from another file whose `country` is
//     REQUIRED with `required` (reported on `Book.location`);
//   - `shelf`: the `Shelf` resource, whose `code` is REQUIRED with `required`
//     (reported once, as Shelf's own).
//
// `CreateBookRequest.parent` and `.book` are REQUIRED with `required`, and are
// a request's fields, so allowed.
func lintResource() map[protoreflect.FullName][]string {
	files, validate := schemaFiles()
	required := rules{"required": true}

	common, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("example/v1/common.proto"),
		Package:    proto.String("example.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/api/field_behavior.proto", "buf/validate/validate.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("Location"),
			Field: []*descriptorpb.FieldDescriptorProto{stringField("country", 1, validated(validate, required, REQUIRED))},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	Expect(files.RegisterFile(common)).To(Succeed())

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("example/v1/book.proto"),
		Package: proto.String("example.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			"google/api/field_behavior.proto",
			"google/api/resource.proto",
			"buf/validate/validate.proto",
			"example/v1/common.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Address"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("city", 1, validated(validate, required, REQUIRED)),
					stringField("street", 2, behavior(REQUIRED)),
				},
			},
			{
				Name:  proto.String("Chapter"),
				Field: []*descriptorpb.FieldDescriptorProto{stringField("title", 1, validated(validate, required, REQUIRED))},
			},
			{
				Name:    proto.String("Shelf"),
				Options: resourceOptions("Shelf", "shelves/{shelf}"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(IDENTIFIER)),
					stringField("code", 2, validated(validate, required, REQUIRED)),
				},
			},
			{
				Name:    proto.String("Book"),
				Options: resourceOptions("Book", "books/{book}"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(IDENTIFIER)),
					stringField("title", 2, validated(validate, rules{"ignore": ignoreZero, "string.min_len": uint64(1)}, REQUIRED)),
					stringField("isbn", 3, validated(validate, required, REQUIRED)),
					stringField("summary", 4, validated(validate, required, OPTIONAL)),
					messageField("address", 5, ".example.v1.Address", behavior(OPTIONAL)),
					messageField("edition", 6, ".example.v1.Book.Edition", behavior(OPTIONAL)),
					repeatedMessageField("chapters", 7, ".example.v1.Chapter"),
					messageField("location", 8, ".example.v1.Location", behavior(OPTIONAL)),
					messageField("shelf", 9, ".example.v1.Shelf", behavior(OPTIONAL)),
				},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:  proto.String("Edition"),
					Field: []*descriptorpb.FieldDescriptorProto{stringField("number", 1, validated(validate, required, REQUIRED))},
				}},
			},
			{
				Name: proto.String("CreateBookRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("parent", 1, validated(validate, required, REQUIRED)),
					messageField("book", 2, ".example.v1.Book", validated(validate, required, REQUIRED)),
				},
			},
		},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	return problemsOf(protoreflect.FullName(linter.ResourceRequiredRejectsZero), file)
}

var _ = Describe("ResourceRequiredRejectsZero", func() {
	Describe("which fields it checks", func() {
		var problems map[protoreflect.FullName][]string

		BeforeEach(func() {
			problems = lintResource()
		})

		It("reports a REQUIRED resource field whose rules reject it unset", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Book.isbn"), ConsistOf(
				ContainSubstring("`required = true` rejects `Book.isbn` when it is unset, but it is REQUIRED on resource `Book`"),
			)))
		})

		It("accepts a REQUIRED resource field whose rules apply only when it is set", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Book.title")))
		})

		It("leaves a field that is not REQUIRED alone", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Book.summary")))
		})

		It("reports a REQUIRED field of a singular message the resource holds", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Address.city"), ConsistOf(
				ContainSubstring("`Address.city` when it is unset, but it is REQUIRED on resource `Book`"),
			)))
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Address.street")))
		})

		It("reports a REQUIRED field of a message declared inside the resource", func() {
			Expect(problems).To(HaveKey(protoreflect.FullName("example.v1.Book.Edition.number")))
		})

		It("accepts the rules of a repeated field's elements", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Chapter.title")))
		})

		It("reports a field declared in another file on the resource field that reaches it", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Book.location"), ConsistOf(
				ContainSubstring("`Book.location.country` reaches `Location.country`, which is REQUIRED, but `required = true` rejects it"),
			)))
		})

		It("reports a field of another resource once, as that resource's", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Shelf.code"), ConsistOf(
				ContainSubstring("REQUIRED on resource `Shelf`"),
			)))
		})

		It("leaves a request's own fields to RequestRequiredUnenforced", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateBookRequest.parent")))
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateBookRequest.book")))
		})

		It("reports nothing else", func() {
			Expect(problems).To(HaveLen(5))
		})
	})

	Describe("which rules reject the zero value", func() {
		str := shape{typ: tString}
		num := func(family string) shape {
			for _, f := range numericFamilies {
				if f.name == family {
					return shape{typ: f.typ}
				}
			}
			panic(family)
		}

		DescribeTable("reported, naming the rule and the fix",
			func(s shape, set rules, rule, fix string) {
				messages := lintResourceValue(s, set)
				Expect(messages).To(ConsistOf(And(
					ContainSubstring(rule+" rejects `Thing.value` when it is unset"),
					ContainSubstring(fix),
					ContainSubstring("ValidateFields"),
				)))
			},
			Entry("required", str, rules{"required": true}, "`required = true`", "remove `required`"),
			Entry("required on an optional field", shape{typ: tString, optional: true}, rules{"required": true}, "`required = true`", "remove `required`"),
			Entry("required under IGNORE_IF_ZERO_VALUE", str, rules{"required": true, "ignore": ignoreZero}, "`required = true`", "remove `required`"),
			Entry("required on a message", shape{typ: tMessage}, rules{"required": true}, "`required = true`", "remove `required`"),
			Entry("string.min_len", str, rules{"string.min_len": uint64(1)}, "`string.min_len = 1`", "IGNORE_IF_ZERO_VALUE` or make the field `optional`"),
			Entry("string.len", str, rules{"string.len": uint64(3)}, "`string.len = 3`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.min_bytes", str, rules{"string.min_bytes": uint64(1)}, "`string.min_bytes = 1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.len_bytes", str, rules{"string.len_bytes": uint64(2)}, "`string.len_bytes = 2`", "IGNORE_IF_ZERO_VALUE"),
			Entry("a pattern \"\" does not match", str, rules{"string.pattern": "^[a-z]+$"}, "`string.pattern = \"^[a-z]+$\"`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.const", str, rules{"string.const": "x"}, "`string.const = \"x\"`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.in without \"\"", str, rules{"string.in": []any{"a", "b"}}, "`string.in = [\"a\", \"b\"]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.not_in with \"\"", str, rules{"string.not_in": []any{""}}, "`string.not_in = [\"\"]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.prefix", str, rules{"string.prefix": "p"}, "`string.prefix = \"p\"`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.email", str, rules{"string.email": true}, "`string.email = true`", "IGNORE_IF_ZERO_VALUE"),
			Entry("string.uuid", str, rules{"string.uuid": true}, "`string.uuid = true`", "IGNORE_IF_ZERO_VALUE"),
			Entry("the HTTP header name regex", str, rules{"string.well_known_regex": protoreflect.EnumNumber(1)}, "`string.well_known_regex = KNOWN_REGEX_HTTP_HEADER_NAME`", "IGNORE_IF_ZERO_VALUE"),
			Entry("bytes.min_len", shape{typ: tBytes}, rules{"bytes.min_len": uint64(1)}, "`bytes.min_len = 1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("bytes.ip", shape{typ: tBytes}, rules{"bytes.ip": true}, "`bytes.ip = true`", "IGNORE_IF_ZERO_VALUE"),
			Entry("bytes.in without empty", shape{typ: tBytes}, rules{"bytes.in": []any{[]byte("a")}}, "`bytes.in = [\"a\"]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("uint32.gt = 0", num("uint32"), rules{"uint32.gt": uint32(0)}, "`uint32.gt = 0`", "IGNORE_IF_ZERO_VALUE"),
			Entry("int32.gte = 1", num("int32"), rules{"int32.gte": int32(1)}, "`int32.gte = 1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("int64.lt = 0", num("int64"), rules{"int64.lt": int64(0)}, "`int64.lt = 0`", "IGNORE_IF_ZERO_VALUE"),
			Entry("double.lte = -1", num("double"), rules{"double.lte": float64(-1)}, "`double.lte = -1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("a range above zero", num("float"), rules{"float.gt": float32(1), "float.lt": float32(10)}, "`float.gt = 1` with `float.lt = 10`", "IGNORE_IF_ZERO_VALUE"),
			Entry("sint32.const", num("sint32"), rules{"sint32.const": int32(5)}, "`sint32.const = 5`", "IGNORE_IF_ZERO_VALUE"),
			Entry("uint64.in without 0", num("uint64"), rules{"uint64.in": []any{uint64(1), uint64(2)}}, "`uint64.in = [1 2]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("sfixed64.not_in with 0", num("sfixed64"), rules{"sfixed64.not_in": []any{int64(0)}}, "`sfixed64.not_in = [0]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("fixed32.gte = 2", num("fixed32"), rules{"fixed32.gte": uint32(2)}, "`fixed32.gte = 2`", "IGNORE_IF_ZERO_VALUE"),
			Entry("enum.const", shape{typ: tEnum}, rules{"enum.const": int32(1)}, "`enum.const = 1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("enum.in without 0", shape{typ: tEnum}, rules{"enum.in": []any{int32(1), int32(2)}}, "`enum.in = [1 2]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("enum.not_in with 0", shape{typ: tEnum}, rules{"enum.not_in": []any{int32(0)}}, "`enum.not_in = [0]`", "IGNORE_IF_ZERO_VALUE"),
			Entry("bool.const = true", shape{typ: tBool}, rules{"bool.const": true}, "`bool.const = true`", "IGNORE_IF_ZERO_VALUE"),
			Entry("repeated.min_items", shape{typ: tString, repeated: true}, rules{"repeated.min_items": uint64(1)}, "`repeated.min_items = 1`", "IGNORE_IF_ZERO_VALUE"),
			Entry("map.min_pairs", shape{isMap: true}, rules{"map.min_pairs": uint64(1)}, "`map.min_pairs = 1`", "IGNORE_IF_ZERO_VALUE"),
		)

		DescribeTable("accepted",
			func(s shape, set rules) {
				Expect(lintResourceValue(s, set)).To(BeEmpty())
			},
			Entry("no rules", str, nil),
			Entry("min_len under IGNORE_IF_ZERO_VALUE", str, rules{"string.min_len": uint64(1), "ignore": ignoreZero}),
			Entry("min_len under an older protovalidate's IGNORE_IF_DEFAULT_VALUE", str, rules{"string.min_len": uint64(1), "ignore": ignoreDefault}),
			Entry("min_len under an ignore value validate.proto does not declare", str, rules{"string.min_len": uint64(1), "ignore": protoreflect.EnumNumber(7)}),
			Entry("required under IGNORE_ALWAYS", str, rules{"required": true, "ignore": ignoreAlways}),
			Entry("min_len on an optional field", shape{typ: tString, optional: true}, rules{"string.min_len": uint64(1)}),
			Entry("min_len on a oneof member", shape{typ: tString, oneof: true}, rules{"string.min_len": uint64(1)}),
			Entry("int32.gt on an optional field", shape{typ: tInt32, optional: true}, rules{"int32.gt": int32(0)}),
			Entry("required = false", str, rules{"required": false}),
			Entry("string.max_len", str, rules{"string.max_len": uint64(10)}),
			Entry("string.len = 0", str, rules{"string.len": uint64(0)}),
			Entry("a pattern \"\" matches", str, rules{"string.pattern": "^[a-z]*$"}),
			Entry("a pattern that does not compile", str, rules{"string.pattern": "("}),
			Entry("string.in with \"\"", str, rules{"string.in": []any{"", "a"}}),
			Entry("string.not_in without \"\"", str, rules{"string.not_in": []any{"a"}}),
			Entry("string.uri_ref, which \"\" satisfies", str, rules{"string.uri_ref": true}),
			Entry("string.email = false", str, rules{"string.email": false}),
			Entry("the HTTP header value regex", str, rules{"string.well_known_regex": protoreflect.EnumNumber(2)}),
			Entry("string.const = \"\"", str, rules{"string.const": ""}),
			Entry("bytes.in with empty", shape{typ: tBytes}, rules{"bytes.in": []any{[]byte{}, []byte("a")}}),
			Entry("int32.gte = 0", num("int32"), rules{"int32.gte": int32(0)}),
			Entry("int64.gt below zero", num("int64"), rules{"int64.gt": int64(-1)}),
			Entry("uint32.lte = 0", num("uint32"), rules{"uint32.lte": uint32(0)}),
			Entry("a range around zero", num("int32"), rules{"int32.gte": int32(-5), "int32.lte": int32(5)}),
			Entry("an exclusive range zero falls outside of", num("int32"), rules{"int32.gt": int32(10), "int32.lt": int32(5)}),
			Entry("a NaN bound", num("double"), rules{"double.gt": math.NaN()}),
			Entry("numeric in with 0", num("uint64"), rules{"uint64.in": []any{uint64(0), uint64(1)}}),
			Entry("enum.defined_only", shape{typ: tEnum}, rules{"enum.defined_only": true}),
			Entry("enum.in with 0", shape{typ: tEnum}, rules{"enum.in": []any{int32(0), int32(1)}}),
			Entry("bool.const = false", shape{typ: tBool}, rules{"bool.const": false}),
			Entry("repeated.min_items = 0", shape{typ: tString, repeated: true}, rules{"repeated.min_items": uint64(0)}),
			Entry("a message without required", shape{typ: tMessage}, rules{"ignore": ignoreZero}),
		)
	})
})
