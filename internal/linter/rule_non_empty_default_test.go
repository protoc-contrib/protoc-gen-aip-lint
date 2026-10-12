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

// defaultsProto is enough of protoc-contrib's defaults options for the rule
// to resolve `(protoc.contrib.defaults.value)` by name. The extension number
// is this fixture's own: the rule never reads it.
func defaultsProto() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String("protoc/contrib/defaults/options.proto"),
		Package:    proto.String("protoc.contrib.defaults"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/descriptor.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("FieldDefault"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   proto.String("int32"),
				Number: proto.Int32(1),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
			}},
		}},
		Extension: []*descriptorpb.FieldDescriptorProto{{
			Name:     proto.String("value"),
			Number:   proto.Int32(51000),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".protoc.contrib.defaults.FieldDefault"),
			Extendee: proto.String(".google.protobuf.FieldOptions"),
		}},
	}
}

// defaulted returns field options with behaviors and
// `(protoc.contrib.defaults.value).int32 = 50`, the default carried as unknown
// fields, as a plugin request carries it.
func defaulted(defaults protoreflect.FileDescriptor, behaviors ...annotations.FieldBehavior) *descriptorpb.FieldOptions {
	extension := defaults.Extensions().ByName("value")
	value := dynamicpb.NewMessage(extension.Message())
	value.Set(extension.Message().Fields().ByName("int32"), protoreflect.ValueOfInt32(50))

	typed := behavior(behaviors...)
	proto.SetExtension(typed, dynamicpb.NewExtensionType(extension), value)
	raw, err := proto.Marshal(typed)
	Expect(err).NotTo(HaveOccurred())
	unknown := &descriptorpb.FieldOptions{}
	Expect(proto.Unmarshal(raw, unknown)).To(Succeed())
	return unknown
}

func int32Field(name string, number int32, options *descriptorpb.FieldOptions) *descriptorpb.FieldDescriptorProto {
	field := stringField(name, number, options)
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()
	return field
}

// lintDefaults returns the problems NonEmptyDefaultValue reports, keyed by
// the full name of the field each is reported on, for:
//
//   - `example.v1.ListBooksRequest`, in a file importing the defaults
//     options: `page_size` is NON_EMPTY_DEFAULT with a default (allowed),
//     `max_page_size` is NON_EMPTY_DEFAULT without one (reported), and
//     `skip` has a default but is OPTIONAL (not this rule's);
//   - `legacy.v1.ListShelvesRequest`, in a file that does not import them:
//     `page_size` is NON_EMPTY_DEFAULT (reported, naming the import), and
//     `filter` is OPTIONAL (nothing to say).
func lintDefaults() map[protoreflect.FullName][]string {
	files := new(protoregistry.Files)
	for _, dependency := range []protoreflect.FileDescriptor{
		descriptorpb.File_google_protobuf_descriptor_proto,
		annotations.File_google_api_field_behavior_proto,
	} {
		Expect(files.RegisterFile(dependency)).To(Succeed())
	}
	defaults, err := protodesc.NewFile(defaultsProto(), files)
	Expect(err).NotTo(HaveOccurred())
	Expect(files.RegisterFile(defaults)).To(Succeed())

	NON_EMPTY_DEFAULT := annotations.FieldBehavior_NON_EMPTY_DEFAULT
	OPTIONAL := annotations.FieldBehavior_OPTIONAL
	books, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("example/v1/book.proto"),
		Package:    proto.String("example.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/api/field_behavior.proto", "protoc/contrib/defaults/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("ListBooksRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{
				int32Field("page_size", 1, defaulted(defaults, OPTIONAL, NON_EMPTY_DEFAULT)),
				int32Field("max_page_size", 2, behavior(OPTIONAL, NON_EMPTY_DEFAULT)),
				int32Field("skip", 3, defaulted(defaults, OPTIONAL)),
			},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())
	shelves, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("legacy/v1/shelf.proto"),
		Package:    proto.String("legacy.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/api/field_behavior.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("ListShelvesRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{
				int32Field("page_size", 1, behavior(OPTIONAL, NON_EMPTY_DEFAULT)),
				stringField("filter", 2, behavior(OPTIONAL)),
			},
		}},
	}, files)
	Expect(err).NotTo(HaveOccurred())

	l, err := linter.New(&linter.Config{})
	Expect(err).NotTo(HaveOccurred())
	responses, err := l.LintProtos(books, shelves)
	Expect(err).NotTo(HaveOccurred())

	problems := map[protoreflect.FullName][]string{}
	for _, response := range responses {
		for _, problem := range response.Problems {
			if problem.RuleID == linter.NonEmptyDefaultValue {
				name := problem.Descriptor.FullName()
				problems[name] = append(problems[name], problem.Message)
			}
		}
	}
	return problems
}

var _ = Describe("NonEmptyDefaultValue", func() {
	var problems map[protoreflect.FullName][]string

	BeforeEach(func() {
		problems = lintDefaults()
	})

	It("accepts a NON_EMPTY_DEFAULT field that sets its default", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.ListBooksRequest.page_size")))
	})

	It("reports a NON_EMPTY_DEFAULT field without a default", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("example.v1.ListBooksRequest.max_page_size"), ConsistOf(
			And(
				ContainSubstring("`ListBooksRequest.max_page_size` is NON_EMPTY_DEFAULT but does not set `(protoc.contrib.defaults.value)`,"),
				Not(ContainSubstring("imported")),
			),
		)))
	})

	It("reports a NON_EMPTY_DEFAULT field in a schema without the defaults options, naming the import", func() {
		Expect(problems).To(HaveKeyWithValue(protoreflect.FullName("legacy.v1.ListShelvesRequest.page_size"), ConsistOf(
			ContainSubstring("which needs `protoc/contrib/defaults/options.proto` imported"),
		)))
	})

	It("leaves fields that are not NON_EMPTY_DEFAULT alone, defaulted or not", func() {
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("example.v1.ListBooksRequest.skip")))
		Expect(problems).NotTo(HaveKey(protoreflect.FullName("legacy.v1.ListShelvesRequest.filter")))
		Expect(problems).To(HaveLen(2))
	})
})
