package linter

import (
	"fmt"

	"github.com/googleapis/api-linter/v2/lint"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// NonEmptyDefaultValue is the name of the rule that has a NON_EMPTY_DEFAULT
// field declare the default it is filled in with.
const NonEmptyDefaultValue lint.RuleName = "protoc-contrib::0203::non-empty-default-value"

// defaultsValueExtension is protoc-contrib's field default option, read by
// protoc-gen-go-defaults and protoc-gen-rust-defaults. It is resolved by name
// from the request's own descriptors, like `buf.validate.field`.
const defaultsValueExtension protoreflect.FullName = "protoc.contrib.defaults.value"

// nonEmptyDefaultValue reports every field annotated
// `(google.api.field_behavior) = NON_EMPTY_DEFAULT` that does not set
// `(protoc.contrib.defaults.value)`.
//
// AIP-203's NON_EMPTY_DEFAULT says the service fills in a non-empty value
// when a client omits the field. Which value is the schema's to say, and
// `(protoc.contrib.defaults.value)` is where it says it: the defaults plugins
// generate the code that fills it in from there. Without it, the annotation
// promises a default nothing applies.
var nonEmptyDefaultValue = &lint.FieldRule{
	Name: NonEmptyDefaultValue,
	OnlyIf: func(field protoreflect.FieldDescriptor) bool {
		return hasBehavior(field, annotations.FieldBehavior_NON_EMPTY_DEFAULT)
	},
	LintField: func(field protoreflect.FieldDescriptor) []lint.Problem {
		if _, ok := fieldOption(field, defaultsValueExtension); ok {
			return nil
		}
		reason := "does not set `(protoc.contrib.defaults.value)`"
		if findExtension(field.ParentFile(), defaultsValueExtension) == nil {
			reason = "does not set `(protoc.contrib.defaults.value)`, which needs `protoc/contrib/defaults/options.proto` imported"
		}
		return []lint.Problem{{
			Message: fmt.Sprintf(
				"`%s.%s` is NON_EMPTY_DEFAULT but %s, so nothing fills in the default it promises: "+
					"set the default there, or drop NON_EMPTY_DEFAULT.",
				field.Parent().Name(), field.Name(), reason),
			Descriptor: field,
		}}
	},
}
