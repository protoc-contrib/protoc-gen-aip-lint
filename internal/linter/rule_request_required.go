package linter

import (
	"fmt"
	"slices"

	"github.com/googleapis/api-linter/v2/lint"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RequestRequiredUnenforced is the name of the rule that has every REQUIRED
// field of a request carry a `buf.validate` rule enforcing it.
const RequestRequiredUnenforced lint.RuleName = "protoc-contrib::0203::request-required-unenforced"

// requestRequiredUnenforced reports every field of an RPC request annotated
// `(google.api.field_behavior) = REQUIRED` that no `(buf.validate.field)`
// rule rejects when a request leaves it unset — the mirror of
// [ResourceRequiredRejectsZero], decided by the same [zeroRejection].
//
// A request message is the input of a method a service in the file declares,
// unless it is a resource, and every message it holds, singular, repeated or
// a map's values. A resource it holds, and what that resource holds, is
// [ResourceRequiredRejectsZero]'s.
//
// A request's own REQUIRED fields — `parent`, the resource field of a create
// or update — have one enforcer: protovalidate, run on every request. A
// REQUIRED field with no rule rejecting its unset value is one the schema
// claims is required and no server checks. A message field is unset only when
// absent, which only `required = true` rejects.
var requestRequiredUnenforced = &lint.FileRule{
	Name: RequestRequiredUnenforced,
	LintFile: func(file protoreflect.FileDescriptor) []lint.Problem {
		var problems []lint.Problem
		reported := map[string]bool{}
		services := file.Services()
		for i := range services.Len() {
			methods := services.Get(i).Methods()
			for j := range methods.Len() {
				method := methods.Get(j)
				request := method.Input()
				if isResource(request) {
					continue
				}
				walkRequest(request, nil, map[protoreflect.FullName]bool{}, func(path []protoreflect.FieldDescriptor) {
					field := path[len(path)-1]
					if rule, _ := zeroRejection(field); rule != "" {
						return
					}
					// A field declared in a file this lint does not cover is
					// reported on the first field of the path in this file, or
					// else on the method, naming the path.
					var anchor protoreflect.Descriptor = method
					if field.ParentFile().Path() == file.Path() {
						anchor = field
					} else {
						for _, step := range path {
							if step.ParentFile().Path() == file.Path() {
								anchor = step
								break
							}
						}
					}
					subject := fmt.Sprintf("`%s.%s`", field.Parent().Name(), field.Name())
					if anchor != protoreflect.Descriptor(field) {
						subject = fmt.Sprintf("`%s`, declared in %s,", joinPath(request, path), field.ParentFile().Path())
					}
					problem := unenforced(field, subject)
					key := string(anchor.FullName()) + " " + string(field.FullName())
					if reported[key] {
						return
					}
					reported[key] = true
					problem.Descriptor = anchor
					problems = append(problems, problem)
				})
			}
		}
		return problems
	},
}

// unenforced is the problem for REQUIRED field, which no rule enforces,
// spelled subject.
func unenforced(field protoreflect.FieldDescriptor, subject string) lint.Problem {
	why := "no `buf.validate` rule rejects it when it is unset"
	if ignoreOfField(field) == ignoreAlways {
		why = "its `ignore = IGNORE_ALWAYS` turns every `buf.validate` rule on it off"
	}
	fix := "add `(buf.validate.field).required = true`, or a rule that fails its zero value"
	if field.Message() != nil && !field.IsList() && !field.IsMap() {
		fix = "add `(buf.validate.field).required = true`, the only rule that fails an absent message"
	}
	return lint.Problem{Message: fmt.Sprintf(
		"%s is REQUIRED on a request, but %s, and protovalidate is the only check a request's own "+
			"REQUIRED fields get: %s.",
		subject, why, fix)}
}

// ignoreOfField is the `ignore` field sets, if any.
func ignoreOfField(field protoreflect.FieldDescriptor) ignoreMode {
	value, ok := fieldOption(field, validateFieldExtension)
	if !ok {
		return ignoreNone
	}
	return ignoreOf(value.Message())
}

// walkRequest calls report with the path to every REQUIRED field of message,
// and of every message it holds, singular, repeated or a map's values. A
// resource is not entered: it is [ResourceRequiredRejectsZero]'s.
func walkRequest(
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
		held := field.Message()
		if field.IsMap() {
			held = field.MapValue().Message()
		}
		if held == nil || isResource(held) {
			continue
		}
		walkRequest(held, at, seen, report)
	}
}
