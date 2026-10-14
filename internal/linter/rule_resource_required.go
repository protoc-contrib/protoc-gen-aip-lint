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

// ResourceRequiredRejectsZero is the name of the rule that keeps
// protovalidate from rejecting the REQUIRED fields of a resource a partial
// update leaves out.
const ResourceRequiredRejectsZero lint.RuleName = "protoc-contrib::0203::resource-required-rejects-zero"

// perMethod is how both rules name the check that does enforce REQUIRED on a
// resource.
const perMethod = "enforce REQUIRED per method (aip-go's FieldBehaviorInterceptor or ValidateFields, aip-rs's FieldBehaviorInterceptor or FieldBehavior::validate_fields)"

// resourceRequiredRejectsZero reports every field of a resource annotated
// `(google.api.field_behavior) = REQUIRED` that protovalidate fails when a
// request leaves it unset, and every such field of a singular message the
// resource holds, at any depth.
//
// AIP-203 has a create set every REQUIRED field, and a partial update omit
// any its mask does not name. The resource message is shared by both, and
// protovalidate cannot tell which method it is validating: a rule that fails
// the field's unset or zero value — `required`, `string.min_len = 1`, a
// pattern "" does not match — fails every partial update that leaves the
// field out. The create/update split is enforced per method instead. Which
// rules fail the zero value is [zeroRejection]'s to decide.
//
// A request's own fields — `parent`, the resource field itself — are not the
// resource's and are not checked: see [RequestRequiredUnenforced]. Nor are the
// fields of a message under a repeated field or a map: a mask cannot index
// into either, so an element is always written whole, and its rules hold for
// create and update alike.
var resourceRequiredRejectsZero = &lint.FileRule{
	Name: ResourceRequiredRejectsZero,
	LintFile: func(file protoreflect.FileDescriptor) []lint.Problem {
		var problems []lint.Problem
		reported := map[protoreflect.FullName]bool{}
		for _, resource := range resources(file.Messages()) {
			walkResource(resource, nil, map[protoreflect.FullName]bool{}, func(path []protoreflect.FieldDescriptor) {
				field := path[len(path)-1]
				rule, required := zeroRejection(field)
				if rule == "" {
					return
				}
				fix := "set `(buf.validate.field).ignore = IGNORE_IF_ZERO_VALUE` or make the field `optional`, and " + perMethod
				if required {
					fix = "remove `required`, and " + perMethod
				}
				if field.ParentFile().Path() == file.Path() {
					if reported[field.FullName()] {
						return
					}
					reported[field.FullName()] = true
					problems = append(problems, lint.Problem{
						Message: fmt.Sprintf(
							"%s rejects `%s.%s` when it is unset, but it is REQUIRED on resource `%s`, which a "+
								"partial update may leave out: %s.",
							rule, field.Parent().Name(), field.Name(), resource.Name(), fix),
						Descriptor: field,
					})
					return
				}
				// Declared in a file this lint does not cover: report it on the
				// resource field the path starts from, which is in this file.
				problems = append(problems, lint.Problem{
					Message: fmt.Sprintf(
						"`%s` reaches `%s.%s`, which is REQUIRED, but %s rejects it when it is unset, as a "+
							"partial update may leave it: there, %s.",
						joinPath(resource, path), field.Parent().Name(), field.Name(), rule, fix),
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

// walkResource calls report with the path to every REQUIRED field of
// message, and of the singular messages it holds. Another resource is not
// entered: it is checked as a resource of its own.
func walkResource(
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
		if hasBehavior(field, annotations.FieldBehavior_REQUIRED) {
			report(at)
		}
		if field.Message() == nil || field.IsList() || field.IsMap() || isResource(field.Message()) {
			continue
		}
		walkResource(field.Message(), at, seen, report)
	}
}

func hasBehavior(field protoreflect.FieldDescriptor, want annotations.FieldBehavior) bool {
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || options == nil {
		return false
	}
	behaviors, _ := proto.GetExtension(options, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	return slices.Contains(behaviors, want)
}

// joinPath spells path as `Message.field.subfield`.
func joinPath(root protoreflect.MessageDescriptor, path []protoreflect.FieldDescriptor) string {
	names := []string{string(root.Name())}
	for _, field := range path {
		names = append(names, string(field.Name()))
	}
	return strings.Join(names, ".")
}
