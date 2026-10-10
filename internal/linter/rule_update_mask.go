package linter

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/googleapis/api-linter/v2/lint"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// UpdateMaskWritableFields is the name of the rule that checks an update
// request's `(buf.validate.field).field_mask.in` against the resource's
// writable fields.
const UpdateMaskWritableFields lint.RuleName = "protoc-contrib::0134::update-mask-writable-fields"

// fieldMaskWildcard is AIP-134's full-replacement path. Listing it in
// `field_mask.in` lets a client ask for full replacement; it names no field.
const fieldMaskWildcard = "*"

// validateFieldExtension is the protovalidate field option. It is resolved by
// name from the request's own descriptors rather than through a Go package,
// so any protovalidate version the schema imports works, and nothing here
// pins one.
const validateFieldExtension protoreflect.FullName = "buf.validate.field"

// updateRequestName is api-linter's own test for an AIP-134 update request.
var updateRequestName = regexp.MustCompile("^Update[A-Z]+[A-Za-z0-9]*Request$")

// customRules are the rules this plugin adds to api-linter's.
var customRules = []lint.ProtoRule{
	updateMaskWritableFields,
}

// registerCustomRules adds customRules to registry under their own
// `protoc-contrib::` namespace. [lint.RuleRegistry.Register] would insist on
// api-linter's `core::` group, which would pass these off as upstream rules —
// and collide with one upstream might add.
func registerCustomRules(registry lint.RuleRegistry) error {
	for _, rule := range customRules {
		name := rule.GetName()
		if !name.IsValid() {
			return fmt.Errorf("invalid rule name %q", name)
		}
		if _, found := registry[name]; found {
			return fmt.Errorf("duplicated rule name %q", name)
		}
		registry[name] = rule
	}
	return nil
}

// updateMaskWritableFields checks that `field_mask.in` on an update request's
// `update_mask` lists exactly the resource's writable fields: every top-level
// field not annotated OUTPUT_ONLY, IDENTIFIER or IMMUTABLE.
//
// The two say the same thing — `google.api.field_behavior` on the resource,
// the allow-list protovalidate enforces on the mask — and nothing else keeps
// them in step. A writable field missing from the list is one no client can
// update; a listed field that is not writable is one a client can overwrite.
// A request with no `field_mask.in` is not this rule's concern.
//
// This is deliberately stricter than AIP-161, which has a server ignore an
// OUTPUT_ONLY path in a mask rather than reject it: here `field_mask.in`
// rejects one, so a client learns its write went nowhere, and this rule keeps
// OUTPUT_ONLY paths out of the list.
var updateMaskWritableFields = &lint.MessageRule{
	Name: UpdateMaskWritableFields,
	OnlyIf: func(m protoreflect.MessageDescriptor) bool {
		return updateRequestName.MatchString(string(m.Name()))
	},
	LintMessage: func(m protoreflect.MessageDescriptor) []lint.Problem {
		mask := m.Fields().ByName("update_mask")
		if mask == nil || mask.Message() == nil || mask.Message().FullName() != "google.protobuf.FieldMask" {
			return nil
		}
		resource := updatedResource(m)
		if resource == nil {
			return nil
		}
		listed, ok := fieldMaskIn(mask)
		if !ok {
			return nil
		}
		return compareWritable(mask, resource, listed)
	},
}

// updatedResource is the message an update request updates: the field whose
// type is named after the request — `book` in `UpdateBookRequest` — or else
// its only singular message field besides `update_mask`.
func updatedResource(request protoreflect.MessageDescriptor) protoreflect.MessageDescriptor {
	want := strings.TrimSuffix(strings.TrimPrefix(string(request.Name()), "Update"), "Request")
	var candidates []protoreflect.MessageDescriptor
	fields := request.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if field.Name() == "update_mask" || field.Message() == nil || field.IsList() || field.IsMap() {
			continue
		}
		if string(field.Message().Name()) == want {
			return field.Message()
		}
		candidates = append(candidates, field.Message())
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
}

// compareWritable reports, as separate problems, every writable field of
// resource missing from listed and every listed path no update may write.
func compareWritable(mask protoreflect.FieldDescriptor, resource protoreflect.MessageDescriptor, listed []string) []lint.Problem {
	writable := writableFields(resource)

	var problems []lint.Problem
	for _, name := range writable {
		if !slices.Contains(listed, name) {
			problems = append(problems, lint.Problem{
				Message: fmt.Sprintf(
					"`%s.%s` is writable but missing from `update_mask`'s `field_mask.in`, so no client can update it: "+
						"add %q, or annotate the field OUTPUT_ONLY, IDENTIFIER or IMMUTABLE.",
					resource.Name(), name, name),
				Descriptor: mask,
			})
		}
	}
	for _, path := range listed {
		if path == fieldMaskWildcard {
			continue
		}
		reason := unwritablePath(resource, path)
		if reason == "" {
			continue
		}
		problems = append(problems, lint.Problem{
			Message: fmt.Sprintf(
				"`update_mask`'s `field_mask.in` lists %q, which %s: remove it.",
				path, reason),
			Descriptor: mask,
		})
	}
	return problems
}

// unwritablePath says why no update may write path on resource, or "" if one
// may. Every segment is checked against the message it indexes: it must be a
// field of it, writable, and — unless it is the last — a singular message,
// since a scalar has no subpaths and a mask cannot index into a repeated field
// or a map.
func unwritablePath(resource protoreflect.MessageDescriptor, path string) string {
	segments := strings.Split(path, ".")
	message := resource
	for i, segment := range segments {
		field := message.Fields().ByName(protoreflect.Name(segment))
		if field == nil {
			if i == 0 {
				return fmt.Sprintf("is not a field of `%s`", message.Name())
			}
			return fmt.Sprintf("reaches %q, which is not a field of `%s`", segment, message.Name())
		}
		if behaviors := unwritableBehaviors(field); len(behaviors) > 0 {
			if i == 0 {
				return fmt.Sprintf("is %s, so no update may write it", strings.Join(behaviors, " and "))
			}
			return fmt.Sprintf("reaches `%s.%s`, which is %s, so no update may write it",
				message.Name(), segment, strings.Join(behaviors, " and "))
		}
		if i == len(segments)-1 {
			return ""
		}
		var kind string
		switch {
		case field.IsMap():
			kind = "a map"
		case field.IsList():
			kind = "a repeated field"
		case field.Message() == nil:
			kind = "a scalar"
		default:
			message = field.Message()
			continue
		}
		return fmt.Sprintf("goes through `%s.%s`, which is %s, not a singular message, so it has no subpaths",
			message.Name(), segment, kind)
	}
	return ""
}

// writableFields is every top-level field of resource an update may write, in
// declaration order — protoc-gen-rust-aip's MUTABLE_PATHS, aip-go's
// MutablePaths.
func writableFields(resource protoreflect.MessageDescriptor) []string {
	var names []string
	fields := resource.Fields()
	for i := range fields.Len() {
		if field := fields.Get(i); len(unwritableBehaviors(field)) == 0 {
			names = append(names, string(field.Name()))
		}
	}
	return names
}

// unwritableBehaviors is the behaviors of field that keep an update from
// writing it.
func unwritableBehaviors(field protoreflect.FieldDescriptor) []string {
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || options == nil {
		return nil
	}
	behaviors, _ := proto.GetExtension(options, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	var found []string
	for _, behavior := range behaviors {
		switch behavior {
		case annotations.FieldBehavior_OUTPUT_ONLY,
			annotations.FieldBehavior_IDENTIFIER,
			annotations.FieldBehavior_IMMUTABLE:
			if name := behavior.String(); !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
	}
	return found
}

// fieldMaskIn reads `(buf.validate.field).field_mask.in` off field, and
// whether it is set at all.
//
// The option is resolved through the extension the request's own descriptors
// declare: protovalidate's Go types are not linked into this binary, so a
// plugin request carries the option as unknown fields, which are re-read here
// against the declared extension.
func fieldMaskIn(field protoreflect.FieldDescriptor) ([]string, bool) {
	extension := findExtension(field.ParentFile(), validateFieldExtension)
	if extension == nil {
		return nil, false
	}
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || options == nil {
		return nil, false
	}
	raw, err := proto.Marshal(options)
	if err != nil {
		return nil, false
	}
	types := new(protoregistry.Types)
	extensionType := dynamicpb.NewExtensionType(extension)
	if err := types.RegisterExtension(extensionType); err != nil {
		return nil, false
	}
	decoded := &descriptorpb.FieldOptions{}
	if err := (proto.UnmarshalOptions{Resolver: types}).Unmarshal(raw, decoded); err != nil {
		return nil, false
	}
	message := decoded.ProtoReflect()
	if !message.Has(extensionType.TypeDescriptor()) {
		return nil, false
	}

	rules := message.Get(extensionType.TypeDescriptor()).Message()
	fieldMask := rules.Descriptor().Fields().ByName("field_mask")
	if fieldMask == nil || !rules.Has(fieldMask) {
		return nil, false
	}
	maskRules := rules.Get(fieldMask).Message()
	in := maskRules.Descriptor().Fields().ByName("in")
	if in == nil || !maskRules.Has(in) {
		return nil, false
	}
	list := maskRules.Get(in).List()
	paths := make([]string, 0, list.Len())
	for i := range list.Len() {
		paths = append(paths, list.Get(i).String())
	}
	return paths, true
}

// findExtension looks for the extension named name in file and everything it
// imports, transitively.
func findExtension(file protoreflect.FileDescriptor, name protoreflect.FullName) protoreflect.ExtensionDescriptor {
	seen := map[string]bool{}
	queue := []protoreflect.FileDescriptor{file}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil || seen[current.Path()] {
			continue
		}
		seen[current.Path()] = true
		if extension := current.Extensions().ByName(name.Name()); extension != nil && extension.FullName() == name {
			return extension
		}
		imports := current.Imports()
		for i := range imports.Len() {
			queue = append(queue, imports.Get(i).FileDescriptor)
		}
	}
	return nil
}
