package linter_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/protoc-contrib/protoc-gen-aip-lint/internal/linter"
)

// validated returns field options with behaviors and `(buf.validate.field)`
// rules set by rules, carried the way a plugin request carries them: the
// field behaviors resolved, protovalidate's rules as unknown fields.
func validated(validate protoreflect.FileDescriptor, rules func(*dynamicpb.Message), behaviors ...annotations.FieldBehavior) *descriptorpb.FieldOptions {
	extension := validate.Extensions().ByName("field")
	message := dynamicpb.NewMessage(extension.Message())
	rules(message)

	typed := behavior(behaviors...)
	proto.SetExtension(typed, dynamicpb.NewExtensionType(extension), message)
	raw, err := proto.Marshal(typed)
	Expect(err).NotTo(HaveOccurred())
	unknown := &descriptorpb.FieldOptions{}
	Expect(proto.Unmarshal(raw, unknown)).To(Succeed())
	return unknown
}

// required sets `required = true`.
func required(rules *dynamicpb.Message) {
	rules.Set(rules.Descriptor().Fields().ByName("required"), protoreflect.ValueOfBool(true))
}

// ignoredMinLen sets `ignore = IGNORE_IF_ZERO_VALUE` and `string.min_len = 1`:
// the length rule a REQUIRED resource field carries in place of `required`.
func ignoredMinLen(rules *dynamicpb.Message) {
	fields := rules.Descriptor().Fields()
	rules.Set(fields.ByName("ignore"), protoreflect.ValueOfEnum(1))
	stringRules := dynamicpb.NewMessage(fields.ByName("string").Message())
	stringRules.Set(stringRules.Descriptor().Fields().ByName("min_len"), protoreflect.ValueOfUint64(1))
	rules.Set(fields.ByName("string"), protoreflect.ValueOfMessage(stringRules))
}

func resourceOptions(kind, pattern string) *descriptorpb.MessageOptions {
	options := &descriptorpb.MessageOptions{}
	proto.SetExtension(options, annotations.E_Resource, &annotations.ResourceDescriptor{
		Type:    "example.com/" + kind,
		Pattern: []string{pattern},
	})
	return options
}

// lintResource builds a `Book` resource and returns the problems
// ResourceRequiredUnvalidated reports for it, keyed by the full name of the
// field each is reported on. `Book` holds:
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

	REQUIRED := annotations.FieldBehavior_REQUIRED
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
					stringField("name", 1, behavior(annotations.FieldBehavior_IDENTIFIER)),
					stringField("code", 2, validated(validate, required, REQUIRED)),
				},
			},
			{
				Name:    proto.String("Book"),
				Options: resourceOptions("Book", "books/{book}"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(annotations.FieldBehavior_IDENTIFIER)),
					stringField("title", 2, validated(validate, ignoredMinLen, REQUIRED)),
					stringField("isbn", 3, validated(validate, required, REQUIRED)),
					stringField("summary", 4, validated(validate, required, annotations.FieldBehavior_OPTIONAL)),
					messageField("address", 5, ".example.v1.Address", behavior(annotations.FieldBehavior_OPTIONAL)),
					messageField("edition", 6, ".example.v1.Book.Edition", behavior(annotations.FieldBehavior_OPTIONAL)),
					repeatedMessageField("chapters", 7, ".example.v1.Chapter"),
					messageField("location", 8, ".example.v1.Location", behavior(annotations.FieldBehavior_OPTIONAL)),
					messageField("shelf", 9, ".example.v1.Shelf", behavior(annotations.FieldBehavior_OPTIONAL)),
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

	l, err := linter.New(&linter.Config{})
	Expect(err).NotTo(HaveOccurred())
	responses, err := l.LintProtos(file)
	Expect(err).NotTo(HaveOccurred())

	problems := map[protoreflect.FullName][]string{}
	for _, response := range responses {
		for _, problem := range response.Problems {
			if problem.RuleID == linter.ResourceRequiredUnvalidated {
				name := problem.Descriptor.FullName()
				problems[name] = append(problems[name], problem.Message)
			}
		}
	}
	return problems
}

var _ = Describe("ResourceRequiredUnvalidated", func() {
	var problems map[protoreflect.FullName][]string

	BeforeEach(func() {
		problems = lintResource()
	})

	It("reports a REQUIRED resource field that sets required", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Book.isbn"), ConsistOf(
			ContainSubstring("`Book.isbn` is REQUIRED on resource `Book`"),
		)))
	})

	It("accepts a REQUIRED resource field whose rules apply only when it is set", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Book.title")))
	})

	It("leaves required on a field that is not REQUIRED alone", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Book.summary")))
	})

	It("reports a REQUIRED field of a singular message the resource holds", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Address.city"), ConsistOf(
			ContainSubstring("`Address.city` is REQUIRED on resource `Book`"),
		)))
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Address.street")))
	})

	It("reports a REQUIRED field of a message declared inside the resource", func() {
		Expect(problems).To(HaveKey(protoreflect.FullName("example.v1.Book.Edition.number")))
	})

	It("accepts required on the elements of a repeated field", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.Chapter.title")))
	})

	It("reports a field declared in another file on the resource field that reaches it", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Book.location"), ConsistOf(
			ContainSubstring("`Book.location.country` reaches `Location.country`, which is REQUIRED"),
		)))
	})

	It("reports a field of another resource once, as that resource's", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.Shelf.code"), ConsistOf(
			ContainSubstring("`Shelf.code` is REQUIRED on resource `Shelf`"),
		)))
	})

	It("accepts required on a request's own fields", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateBookRequest.parent")))
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.CreateBookRequest.book")))
	})

	It("reports nothing else", func() {
		Expect(problems).To(HaveLen(5))
	})
})
