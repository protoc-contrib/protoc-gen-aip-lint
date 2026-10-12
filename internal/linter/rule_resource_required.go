package linter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/googleapis/api-linter/v2/lint"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ResourceRequiredUnvalidated is the name of the rule that keeps
// `(buf.validate.field).required` off the REQUIRED fields of a resource.
const ResourceRequiredUnvalidated lint.RuleName = "protoc-contrib::0203::resource-required-unvalidated"

// resourceRequiredUnvalidated reports every field of a resource annotated
// `(google.api.field_behavior) = REQUIRED` that also sets
// `(buf.validate.field).required = true`, and every such field of a singular
// message the resource holds, at any depth.
//
// AIP-203 has a create set every REQUIRED field, and a partial update omit
// any its mask does not name. The resource message is shared by both, and
// protovalidate cannot tell which method it is validating, so `required` on
// the resource rejects every partial update that leaves the field out. The
// create/update split is enforced per method instead — aip-go's
// MissingRequiredPaths, protoc-gen-rust-aip's missing_required_paths.
//
// A request's own fields — `parent`, the resource field itself — are not the
// resource's and are not checked: protovalidate's `required` is right there.
// Nor are the fields of a message under a repeated field or a map: a mask
// cannot index into either, so an element is always written whole, and
// `required` on it holds for create and update alike.
var resourceRequiredUnvalidated = &lint.FileRule{
	Name: ResourceRequiredUnvalidated,
	LintFile: func(file protoreflect.FileDescriptor) []lint.Problem {
		var problems []lint.Problem
		reported := map[protoreflect.FullName]bool{}
		for _, resource := range resources(file.Messages()) {
			walkRequiredValidated(resource, nil, map[protoreflect.FullName]bool{}, func(path []protoreflect.FieldDescriptor) {
				field := path[len(path)-1]
				if field.ParentFile().Path() == file.Path() {
					if reported[field.FullName()] {
						return
					}
					reported[field.FullName()] = true
					problems = append(problems, lint.Problem{
						Message: fmt.Sprintf(
							"`%s.%s` is REQUIRED on resource `%s`, which a partial update may omit, but "+
								"`(buf.validate.field).required` rejects any request without it: remove `required`, "+
								"and enforce REQUIRED per method (aip-go's MissingRequiredPaths).",
							field.Parent().Name(), field.Name(), resource.Name()),
						Descriptor: field,
					})
					return
				}
				// Declared in a file this lint does not cover: report it on the
				// resource field the path starts from, which is in this file.
				problems = append(problems, lint.Problem{
					Message: fmt.Sprintf(
						"`%s` reaches `%s.%s`, which is REQUIRED, but `(buf.validate.field).required` rejects any "+
							"partial update without it: remove `required` there, and enforce REQUIRED per method "+
							"(aip-go's MissingRequiredPaths).",
						joinPath(resource, path), field.Parent().Name(), field.Name()),
					Descriptor: path[0],
				})
			})
		}
		return problems
	},
}

// resources is every message among messages, and the messages nested in
// them, that declares `google.api.resource`.
func resources(messages protoreflect.MessageDescriptors) []protoreflect.MessageDescriptor {
	var found []protoreflect.MessageDescriptor
	for i := range messages.Len() {
		message := messages.Get(i)
		if isResource(message) {
			found = append(found, message)
		}
		found = append(found, resources(message.Messages())...)
	}
	return found
}

func isResource(message protoreflect.MessageDescriptor) bool {
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil {
		return false
	}
	return proto.HasExtension(options, annotations.E_Resource)
}

// walkRequiredValidated calls report with the path to every field of message,
// and of the singular messages it holds, that is REQUIRED and sets
// `(buf.validate.field).required = true`. Another resource is not entered:
// it is checked as a resource of its own.
func walkRequiredValidated(
	message protoreflect.MessageDescriptor,
	path []protoreflect.FieldDescriptor,
	seen map[protoreflect.FullName]bool,
	report func([]protoreflect.FieldDescriptor),
) {
	if seen[message.FullName()] {
		return
	}
	seen[message.FullName()] = true
	fields := message.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		at := append(slices.Clip(path), field)
		if hasBehavior(field, annotations.FieldBehavior_REQUIRED) && validateRequired(field) {
			report(at)
		}
		if field.Message() == nil || field.IsList() || field.IsMap() || isResource(field.Message()) {
			continue
		}
		walkRequiredValidated(field.Message(), at, seen, report)
	}
}

// validateRequired reports whether field sets `(buf.validate.field).required`
// to true.
func validateRequired(field protoreflect.FieldDescriptor) bool {
	value, ok := fieldOption(field, validateFieldExtension)
	if !ok {
		return false
	}
	rules := value.Message()
	required := rules.Descriptor().Fields().ByName("required")
	return required != nil && rules.Has(required) && rules.Get(required).Bool()
}

func hasBehavior(field protoreflect.FieldDescriptor, want annotations.FieldBehavior) bool {
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || options == nil {
		return false
	}
	behaviors, _ := proto.GetExtension(options, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	return slices.Contains(behaviors, want)
}

// joinPath spells path as `Resource.field.subfield`.
func joinPath(resource protoreflect.MessageDescriptor, path []protoreflect.FieldDescriptor) string {
	names := []string{string(resource.Name())}
	for _, field := range path {
		names = append(names, string(field.Name()))
	}
	return strings.Join(names, ".")
}
