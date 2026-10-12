package linter_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/protoc-contrib/protoc-gen-aip-lint/internal/linter"
)

func method(name, input string) *descriptorpb.MethodDescriptorProto {
	return &descriptorpb.MethodDescriptorProto{
		Name:       proto.String(name),
		InputType:  proto.String(input),
		OutputType: proto.String(".example.v1.Reply"),
	}
}

// lintRequestValue lints `DoThingRequest`, the input of `Things.DoThing`,
// whose only field is `value`, REQUIRED, of shape s, with the given rules,
// and returns what RequestRequiredUnenforced reports on it.
func lintRequestValue(s shape, set rules) []string {
	files, validate := schemaFiles()
	enums, sub := supporting()
	request := &descriptorpb.DescriptorProto{Name: proto.String("DoThingRequest")}
	options := behavior(REQUIRED)
	if set != nil {
		options = validated(validate, set, REQUIRED)
	}
	declare(request, s, options)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        proto.String("example/v1/thing.proto"),
		Package:     proto.String("example.v1"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"google/api/field_behavior.proto", "buf/validate/validate.proto"},
		MessageType: []*descriptorpb.DescriptorProto{sub, request, {Name: proto.String("Reply")}},
		EnumType:    enums,
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:   proto.String("Things"),
			Method: []*descriptorpb.MethodDescriptorProto{method("DoThing", ".example.v1.DoThingRequest")},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	return problemsOf(protoreflect.FullName(linter.RequestRequiredUnenforced), file)["example.v1.DoThingRequest.value"]
}

// lintRequests lints the `Library` service and returns what
// RequestRequiredUnenforced reports. Its requests hold:
//
//   - ListCollectionsRequest: `parent` REQUIRED with only a pattern ""
//     fails (enforced), `filter` REQUIRED with no rules (reported),
//     `page_size` OPTIONAL with none (not this rule's);
//   - CreateCollectionRequest: `parent` and the resource field `collection`,
//     both REQUIRED with `required` (enforced);
//   - UpdateCollectionRequest: `collection` REQUIRED without `required`
//     (reported); `secret` REQUIRED with `required` but IGNORE_ALWAYS
//     (reported); a singular `UpdateOptions` whose `reason` is REQUIRED with
//     no rules (reported); repeated `Label`s whose `key` is enforced and
//     `value` is not (reported); a map of `Attr`s whose `name` is not
//     (reported);
//   - GetShelf takes the `Shelf` resource itself, whose REQUIRED `code` has
//     no rules (not this rule's), as is `Collection.title`, reached through
//     `collection`;
//   - Import takes `ImportRequest`, declared in another file, whose REQUIRED
//     `source` has no rules (reported on the method).
func lintRequests() map[protoreflect.FullName][]string {
	files, validate := schemaFiles()
	required := rules{"required": true}

	imports, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("example/v1/imports.proto"),
		Package:    proto.String("example.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/api/field_behavior.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("ImportRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{stringField("source", 1, behavior(REQUIRED))},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	Expect(files.RegisterFile(imports)).To(Succeed())

	attrs := repeatedMessageField("attrs", 6, ".example.v1.UpdateCollectionRequest.AttrsEntry")
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("example/v1/library.proto"),
		Package: proto.String("example.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			"google/api/field_behavior.proto",
			"google/api/resource.proto",
			"buf/validate/validate.proto",
			"example/v1/imports.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Reply")},
			{
				Name:    proto.String("Collection"),
				Options: resourceOptions("Collection", "collections/{collection}"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(IDENTIFIER)),
					stringField("title", 2, behavior(REQUIRED)),
				},
			},
			{
				Name:    proto.String("Shelf"),
				Options: resourceOptions("Shelf", "shelves/{shelf}"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(IDENTIFIER)),
					stringField("code", 2, behavior(REQUIRED)),
				},
			},
			{
				Name: proto.String("ListCollectionsRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("parent", 1, validated(validate, rules{"string.pattern": "^collections/.+$"}, REQUIRED)),
					stringField("filter", 2, behavior(REQUIRED)),
					stringField("page_size", 3, behavior(OPTIONAL)),
				},
			},
			{
				Name: proto.String("CreateCollectionRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("parent", 1, validated(validate, required, REQUIRED)),
					messageField("collection", 2, ".example.v1.Collection", validated(validate, required, REQUIRED)),
				},
			},
			{
				Name: proto.String("UpdateOptions"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("reason", 1, behavior(REQUIRED)),
				},
			},
			{
				Name: proto.String("Label"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("key", 1, validated(validate, rules{"string.min_len": uint64(1)}, REQUIRED)),
					stringField("value", 2, behavior(REQUIRED)),
				},
			},
			{
				Name:  proto.String("Attr"),
				Field: []*descriptorpb.FieldDescriptorProto{stringField("name", 1, behavior(REQUIRED))},
			},
			{
				Name: proto.String("UpdateCollectionRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					messageField("collection", 1, ".example.v1.Collection", behavior(REQUIRED)),
					stringField("secret", 2, validated(validate, rules{"required": true, "ignore": ignoreAlways}, REQUIRED)),
					messageField("options", 3, ".example.v1.UpdateOptions", behavior(OPTIONAL)),
					repeatedMessageField("labels", 4, ".example.v1.Label"),
					attrs,
				},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:    proto.String("AttrsEntry"),
					Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					Field: []*descriptorpb.FieldDescriptorProto{
						ruleField("key", 1, tString, "", false),
						ruleField("value", 2, tMessage, ".example.v1.Attr", false),
					},
				}},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Library"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("ListCollections", ".example.v1.ListCollectionsRequest"),
				method("CreateCollection", ".example.v1.CreateCollectionRequest"),
				method("UpdateCollection", ".example.v1.UpdateCollectionRequest"),
				method("GetShelf", ".example.v1.Shelf"),
				method("Import", ".example.v1.ImportRequest"),
			},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	return problemsOf(protoreflect.FullName(linter.RequestRequiredUnenforced), file)
}

var _ = Describe("RequestRequiredUnenforced", func() {
	Describe("which fields it checks", func() {
		var problems map[protoreflect.FullName][]string

		BeforeEach(func() {
			problems = lintRequests()
		})

		It("accepts a REQUIRED field with required", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateCollectionRequest.parent")))
		})

		It("accepts a REQUIRED field whose pattern fails \"\"", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.ListCollectionsRequest.parent")))
		})

		It("reports a REQUIRED field with no rules", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.ListCollectionsRequest.filter"), ConsistOf(
				ContainSubstring("`ListCollectionsRequest.filter` is REQUIRED on a request, but no `buf.validate` rule rejects it when it is unset"),
			)))
		})

		It("leaves an OPTIONAL field alone", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.ListCollectionsRequest.page_size")))
		})

		It("accepts a REQUIRED resource field with required", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateCollectionRequest.collection")))
		})

		It("reports a REQUIRED resource field without required, which alone fails an absent message", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.UpdateCollectionRequest.collection"), ConsistOf(
				ContainSubstring("the only rule that fails an absent message"),
			)))
		})

		It("reports a REQUIRED field whose rules IGNORE_ALWAYS turns off", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.UpdateCollectionRequest.secret"), ConsistOf(
				ContainSubstring("its `ignore = IGNORE_ALWAYS` turns every `buf.validate` rule on it off"),
			)))
		})

		It("checks the messages a request holds, singular, repeated or a map's values", func() {
			Expect(problems).To(HaveKey(protoreflect.FullName("example.v1.UpdateOptions.reason")))
			Expect(problems).To(HaveKey(protoreflect.FullName("example.v1.Label.value")))
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Label.key")))
			Expect(problems).To(HaveKey(protoreflect.FullName("example.v1.Attr.name")))
		})

		It("leaves a resource, as input or held, to ResourceRequiredRejectsZero", func() {
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Shelf.code")))
			Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Collection.title")))
		})

		It("reports a request declared in another file on the method", func() {
			Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Library.Import"), ConsistOf(
				ContainSubstring("`ImportRequest.source`, declared in example/v1/imports.proto, is REQUIRED on a request"),
			)))
		})

		It("reports nothing else", func() {
			Expect(problems).To(HaveLen(7))
		})
	})

	DescribeTable("shares ResourceRequiredRejectsZero's reading of the rules, inverted",
		func(s shape, set rules, enforced bool) {
			messages := lintRequestValue(s, set)
			if enforced {
				Expect(messages).To(BeEmpty())
			} else {
				Expect(messages).To(ConsistOf(ContainSubstring("`DoThingRequest.value` is REQUIRED on a request")))
			}
		},
		Entry("no rules", shape{typ: tString}, nil, false),
		Entry("string.min_len = 1", shape{typ: tString}, rules{"string.min_len": uint64(1)}, true),
		Entry("string.min_len = 1 under IGNORE_IF_ZERO_VALUE", shape{typ: tString}, rules{"string.min_len": uint64(1), "ignore": ignoreZero}, false),
		Entry("required under IGNORE_IF_ZERO_VALUE", shape{typ: tString}, rules{"required": true, "ignore": ignoreZero}, true),
		Entry("a pattern \"\" matches", shape{typ: tString}, rules{"string.pattern": "^[a-z]*$"}, false),
		Entry("uint32.gt = 0", shape{typ: numericFamilies[4].typ}, rules{"uint32.gt": uint32(0)}, true),
		Entry("int32.gte = 0", shape{typ: tInt32}, rules{"int32.gte": int32(0)}, false),
		Entry("an optional field with min_len", shape{typ: tString, optional: true}, rules{"string.min_len": uint64(1)}, false),
		Entry("an optional field with required", shape{typ: tString, optional: true}, rules{"required": true}, true),
		Entry("repeated.min_items = 1", shape{typ: tString, repeated: true}, rules{"repeated.min_items": uint64(1)}, true),
		Entry("enum.defined_only", shape{typ: tEnum}, rules{"enum.defined_only": true}, false),
		Entry("enum.not_in with 0", shape{typ: tEnum}, rules{"enum.not_in": []any{int32(0)}}, true),
	)
})
