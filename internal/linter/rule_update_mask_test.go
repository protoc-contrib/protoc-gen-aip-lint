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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/protoc-contrib/protoc-gen-aip-lint/internal/linter"
)

// validateProto is enough of protovalidate's validate.proto for the rule to
// resolve `(buf.validate.field).field_mask.in` by name.
func validateProto() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String("buf/validate/validate.proto"),
		Package:    proto.String("buf.validate"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/descriptor.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("FieldMaskRules"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("in"),
					Number: proto.Int32(2),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: proto.String("FieldRules"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:     proto.String("field_mask"),
					Number:   proto.Int32(28),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
					TypeName: proto.String(".buf.validate.FieldMaskRules"),
				}},
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

// maskOptions carries `field_mask.in = paths` the way a plugin request does:
// as unknown fields, protovalidate's Go types not being linked.
func maskOptions(validate protoreflect.FileDescriptor, paths []string) *descriptorpb.FieldOptions {
	extension := validate.Extensions().ByName("field")
	extensionType := dynamicpb.NewExtensionType(extension)
	rules := dynamicpb.NewMessage(extension.Message())
	fieldMask := extension.Message().Fields().ByName("field_mask")
	maskRules := dynamicpb.NewMessage(fieldMask.Message())
	list := maskRules.Mutable(fieldMask.Message().Fields().ByName("in")).List()
	for _, path := range paths {
		list.Append(protoreflect.ValueOfString(path))
	}
	rules.Set(fieldMask, protoreflect.ValueOfMessage(maskRules))

	typed := &descriptorpb.FieldOptions{}
	proto.SetExtension(typed, extensionType, rules)
	raw, err := proto.Marshal(typed)
	Expect(err).NotTo(HaveOccurred())
	unknown := &descriptorpb.FieldOptions{}
	Expect(proto.Unmarshal(raw, unknown)).To(Succeed())
	return unknown
}

func behavior(behaviors ...annotations.FieldBehavior) *descriptorpb.FieldOptions {
	options := &descriptorpb.FieldOptions{}
	proto.SetExtension(options, annotations.E_FieldBehavior, behaviors)
	return options
}

func stringField(name string, number int32, options *descriptorpb.FieldOptions) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    proto.String(name),
		Number:  proto.Int32(number),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Options: options,
	}
}

func messageField(name string, number int32, typeName string, options *descriptorpb.FieldOptions) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(number),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
		Options:  options,
	}
}

func repeatedMessageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	field := messageField(name, number, typeName, nil)
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return field
}

// lintUpdate builds `UpdateBookRequest` over a `Book` with one field of each
// kind — writable (`title`, the message `author`, the repeated `chapters`),
// IDENTIFIER (`name`), OUTPUT_ONLY (`etag`), IMMUTABLE (`serial`) — where
// `Author` has a writable `name` and an OUTPUT_ONLY `create_time`, and returns
// the messages
// UpdateMaskWritableFields reports for it. A nil paths leaves `field_mask.in`
// unset.
func lintUpdate(paths []string) []string {
	files := new(protoregistry.Files)
	for _, dependency := range []protoreflect.FileDescriptor{
		descriptorpb.File_google_protobuf_descriptor_proto,
		annotations.File_google_api_field_behavior_proto,
		fieldmaskpb.File_google_protobuf_field_mask_proto,
	} {
		Expect(files.RegisterFile(dependency)).To(Succeed())
	}
	validate, err := protodesc.NewFile(validateProto(), files)
	Expect(err).NotTo(HaveOccurred())
	Expect(files.RegisterFile(validate)).To(Succeed())

	var maskOpts *descriptorpb.FieldOptions
	if paths != nil {
		maskOpts = maskOptions(validate, paths)
	}

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("example/v1/book.proto"),
		Package: proto.String("example.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			"google/api/field_behavior.proto",
			"google/protobuf/field_mask.proto",
			"buf/validate/validate.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Author"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, nil),
					stringField("create_time", 2, behavior(annotations.FieldBehavior_OUTPUT_ONLY)),
				},
			},
			{
				Name:  proto.String("Chapter"),
				Field: []*descriptorpb.FieldDescriptorProto{stringField("title", 1, nil)},
			},
			{
				Name: proto.String("Book"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("name", 1, behavior(annotations.FieldBehavior_IDENTIFIER)),
					stringField("title", 2, nil),
					messageField("author", 3, ".example.v1.Author", behavior(annotations.FieldBehavior_OPTIONAL)),
					stringField("etag", 4, behavior(annotations.FieldBehavior_OUTPUT_ONLY)),
					stringField("serial", 5, behavior(annotations.FieldBehavior_IMMUTABLE)),
					repeatedMessageField("chapters", 6, ".example.v1.Chapter"),
				},
			},
			{
				Name: proto.String("UpdateBookRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					messageField("book", 1, ".example.v1.Book", nil),
					messageField("update_mask", 2, ".google.protobuf.FieldMask", maskOpts),
				},
			},
		},
	}, files)
	Expect(err).NotTo(HaveOccurred())

	l, err := linter.New(&linter.Config{})
	Expect(err).NotTo(HaveOccurred())
	responses, err := l.LintProtos(file)
	Expect(err).NotTo(HaveOccurred())

	var messages []string
	for _, response := range responses {
		for _, problem := range response.Problems {
			if problem.RuleID == linter.UpdateMaskWritableFields {
				Expect(problem.Descriptor.FullName()).To(Equal(protoreflect.FullName("example.v1.UpdateBookRequest.update_mask")))
				messages = append(messages, problem.Message)
			}
		}
	}
	return messages
}

var _ = Describe("UpdateMaskWritableFields", func() {
	It("accepts field_mask.in listing exactly the writable fields, in any order", func() {
		Expect(lintUpdate([]string{"chapters", "author", "title"})).To(BeEmpty())
	})

	It("accepts a nested path under a writable field", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "author.name"})).To(BeEmpty())
	})

	It("accepts the full-replacement wildcard", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "*"})).To(BeEmpty())
	})

	It("says nothing when field_mask.in is not set", func() {
		Expect(lintUpdate(nil)).To(BeEmpty())
	})

	It("treats an empty field_mask.in as unset", func() {
		// An empty repeated field does not survive encoding: it is the same
		// options as no `in` at all.
		Expect(lintUpdate([]string{})).To(BeEmpty())
	})

	It("reports each writable field missing from field_mask.in", func() {
		// `author.name` allows a path under `author`, not `author` itself.
		messages := lintUpdate([]string{"author.name"})
		Expect(messages).To(HaveLen(3))
		Expect(messages[0]).To(ContainSubstring("`Book.title` is writable but missing"))
		Expect(messages[0]).To(ContainSubstring(`add "title"`))
		Expect(messages[1]).To(ContainSubstring("`Book.author` is writable but missing"))
		Expect(messages[2]).To(ContainSubstring("`Book.chapters` is writable but missing"))
	})

	It("reports each listed field an update may not write, naming why", func() {
		messages := lintUpdate([]string{"title", "author", "chapters", "name", "etag", "serial"})
		Expect(messages).To(ConsistOf(
			ContainSubstring(`lists "name", which is IDENTIFIER`),
			ContainSubstring(`lists "etag", which is OUTPUT_ONLY`),
			ContainSubstring(`lists "serial", which is IMMUTABLE`),
		))
	})

	It("reports a listed path that is not a field at all", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "pages"})).To(ConsistOf(
			ContainSubstring(`lists "pages", which is not a field of ` + "`Book`"),
		))
	})

	It("reports a nested path under a field an update may not write", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "etag.value"})).To(ConsistOf(
			ContainSubstring(`lists "etag.value", which is OUTPUT_ONLY`),
		))
	})

	It("reports a nested path whose last segment an update may not write", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "author.create_time"})).To(ConsistOf(
			ContainSubstring(`lists "author.create_time", which reaches ` + "`Author.create_time`, which is OUTPUT_ONLY"),
		))
	})

	It("reports a nested path naming no field of the message it indexes", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "author.pages"})).To(ConsistOf(
			ContainSubstring(`lists "author.pages", which reaches "pages", which is not a field of ` + "`Author`"),
		))
	})

	It("reports a path through a scalar", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "title.length"})).To(ConsistOf(
			ContainSubstring(`lists "title.length", which goes through ` + "`Book.title`, which is a scalar"),
		))
	})

	It("reports a path through a repeated field", func() {
		Expect(lintUpdate([]string{"title", "author", "chapters", "chapters.title"})).To(ConsistOf(
			ContainSubstring(`lists "chapters.title", which goes through ` + "`Book.chapters`, which is a repeated field"),
		))
	})
})
