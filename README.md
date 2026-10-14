# protoc-gen-aip-lint

[![CI](https://github.com/protoc-contrib/protoc-gen-aip-lint/actions/workflows/ci.yml/badge.svg)](https://github.com/protoc-contrib/protoc-gen-aip-lint/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/protoc-contrib/protoc-gen-aip-lint?include_prereleases)](https://github.com/protoc-contrib/protoc-gen-aip-lint/releases)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![protoc](https://img.shields.io/badge/protoc-compatible-blue)](https://protobuf.dev)

A [protoc](https://protobuf.dev) plugin that runs the [Google API Linter](https://github.com/googleapis/api-linter) on your Protocol Buffer files. It checks `.proto` files against the [AIP](https://aip.dev) style guidelines and reports problems in multiple output formats.

## Features

- Lint `.proto` files against the full set of AIP rules
- Rules of its own that keep `buf.validate` in step with `google.api.field_behavior` (see [Custom Rules](#custom-rules))
- Multiple output formats: YAML, JSON, GitHub Actions annotations, and summary table
- Configurable via the standard `api-linter` YAML configuration file
- Works with `protoc`, `buf`, or any protoc-compatible toolchain
- CI-friendly `set-exit-status` option for gating builds on lint failures

## Installation

```bash
go install github.com/protoc-contrib/protoc-gen-aip-lint/cmd/protoc-gen-aip-lint@latest
```

## Usage

### With buf

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - protoc_builtin: cpp
    out: gen/proto/cpp
  - local: protoc-gen-aip-lint
    out: .
    opt:
      - output-format=github
      - set-exit-status
```

Then run:

```bash
buf generate
```

### With protoc

```bash
protoc \
  --aip-lint_out=. \
  --aip-lint_opt=output-format=github,set-exit-status \
  -I proto/ \
  proto/example/v1/example.proto
```

## Plugin Options

Options are passed via `--aip-lint_opt` (protoc) or `opt` (buf). Multiple
options are comma-separated.

| Option                    | Description                                                        |
| ------------------------- | ------------------------------------------------------------------ |
| `config`                  | Path to the `api-linter` YAML configuration file                   |
| `ignore-comment-disables` | Ignore inline disable comments for strict AIP enforcement          |
| `output-format`           | Output format: `yaml` (default), `json`, `github`, or `summary`    |
| `output-path`             | Output file path (defaults to stderr; stdout is reserved for protoc) |
| `set-exit-status`         | Exit with non-zero status when lint problems are found             |

## Output Formats

### YAML (default)

```yaml
- filepath: example.proto
  problems:
    - message: "Field must be in lower_snake_case."
      rule_id: "core::0140::lower-snake"
```

### JSON

```json
[
  {
    "file_path": "example.proto",
    "problems": [
      {
        "message": "Field must be in lower_snake_case.",
        "rule_id": "core::0140::lower-snake"
      }
    ]
  }
]
```

### GitHub Actions

Emits `::error` workflow commands that appear as inline annotations on pull
request diffs:

```
::error file=example.proto,line=10,col=3::core::0140::lower-snake: Field must be in lower_snake_case.
```

### Summary

A human-readable table with problem counts per rule:

```
RULE                                      COUNT
----------------------------------------  -----
core::0123::resource-pattern              1
core::0140::lower-snake                   3
----------------------------------------  -----
Total (2 files)                           4
```

## Custom Rules

Alongside api-linter's rules, the plugin runs its own, under a
`protoc-contrib::` namespace so they are never mistaken for upstream ones.
Disable one like any other rule.

### `protoc-contrib::0134::update-mask-writable-fields`

On an AIP-134 `Update*Request`, the `(buf.validate.field).field_mask.in`
allow-list on `update_mask` must list exactly the updated resource's writable
fields: every top-level field not annotated `OUTPUT_ONLY`, `IDENTIFIER` or
`IMMUTABLE`.

```proto
google.protobuf.FieldMask update_mask = 2 [
  (buf.validate.field).field_mask = {in: ["display_name", "description"]}
];
```

The list restates what `google.api.field_behavior` already says, and nothing
else keeps the two in step. A writable field missing from it is one no client
can update; a listed field that is not writable is one a client can
overwrite. Each is reported separately, naming the field. Order does not
matter, and the full-replacement wildcard `*` may be listed.

A nested path such as `author.name` is checked segment by segment: each must
be a field of the message it indexes and writable, and each but the last a
singular message — a mask cannot index into a scalar, a repeated field or a
map. So `author.create_time` is reported when `Author.create_time` is
`OUTPUT_ONLY`, however writable `author` is.

Keeping `OUTPUT_ONLY` paths out of the list makes `field_mask.in` alone
**stricter than AIP-161**, which has a server ignore such a path in a mask
rather than reject it: protovalidate rejects the update instead. A server
that follows AIP-161 drops the path before protovalidate runs — aip-go's
`FieldBehaviorInterceptor` does, with
`ClearFieldMask(resource, aip.OutputOnly, mask)`, and so does aip-rs's (0.6 and
later), with `Resource::clear_field_mask(FieldBehaviorKind::OutputOnly, mask)`
— so the list never sees it either way.

A request whose `update_mask` has no `field_mask.in` is not checked. The
resource is the request field typed after it (`book` in `UpdateBookRequest`),
or else its only message field besides `update_mask`. The option is read
through the `buf/validate/validate.proto` the schema imports, so any
protovalidate version works.

### `protoc-contrib::0203::resource-required-rejects-zero`

A field of a resource annotated `(google.api.field_behavior) = REQUIRED`
must not carry a `buf.validate` rule that fails when a request leaves the
field unset. Nor must a REQUIRED field of a singular message the resource
holds, at any depth.

```proto
message Book {
  option (google.api.resource) = {type: "example.com/Book" pattern: "books/{book}"};

  string title = 2 [
    (google.api.field_behavior) = REQUIRED,
    // Not `required`, and not min_len alone: the length rule applies once a
    // value is set.
    (buf.validate.field).ignore = IGNORE_IF_ZERO_VALUE,
    (buf.validate.field).string.min_len = 1
  ];
}
```

[AIP-203](https://google.aip.dev/203#required) has a create set every
REQUIRED field, and a partial update omit any its `update_mask` does not
name. The resource message is shared by both, and protovalidate cannot tell
which method it is validating, so a rule that fails the field's unset value
fails every partial update that leaves the field out. Enforce the
create/update split per method instead: aip-go's `FieldBehaviorInterceptor`
does, or call `ValidateFields(resource, aip.Required, mask)` yourself — a nil
mask on a create checks every field, an update's mask only the fields it
names. In Rust, aip-rs's `FieldBehaviorInterceptor` does the same, or call
`FieldBehavior::validate_fields(FieldBehaviorKind::Required, scope)` with
`FieldScope::All` on a create and `FieldScope::Mask` on an update.

The problem names the rule and the fix. A rule fails the unset value, in
protovalidate's own order, when:

- it is `required = true`, which fails an unset field whatever its presence
  and any `ignore` but `IGNORE_ALWAYS` (fix: remove it); or
- the field has no presence (not `optional`, a message or a oneof member),
  its `ignore` is neither `IGNORE_IF_ZERO_VALUE` nor `IGNORE_ALWAYS` (nor an
  older protovalidate's `IGNORE_IF_UNPOPULATED`, `IGNORE_EMPTY`,
  `IGNORE_IF_DEFAULT_VALUE` or `IGNORE_DEFAULT`), and a type rule fails the
  zero value (fix: `IGNORE_IF_ZERO_VALUE`, or make the field `optional`):

| Type | Fails the zero value |
| --- | --- |
| `string`, `bytes` | `len`, `min_len` (and `len_bytes`, `min_bytes`) ≥ 1; a `pattern` that does not match `""`; a non-empty `const`, `prefix`, `suffix` or `contains`; `in` without the empty value; `not_in` with it; `string.not_contains = ""`; a well-known format `""` fails — every `string` one but `uri_ref`, `well_known_regex = KNOWN_REGEX_HTTP_HEADER_NAME`, and `bytes` `ip`, `ipv4`, `ipv6`, `uuid` |
| numeric | `const` ≠ 0; `in` without 0; `not_in` with 0; bounds that exclude 0, combined as protovalidate combines them (both when the upper is at least the lower, either when below) |
| `enum` | `const` ≠ 0; `in` without 0; `not_in` with 0 |
| `bool` | `const = true` |
| `repeated`, `map` | `min_items`, `min_pairs` ≥ 1 |

Only what can be decided exactly is reported: a pattern is compiled as RE2
and tried on `""`, a NaN bound or an `ignore` value the schema's
validate.proto does not declare is left alone, and `cel` rules are never
evaluated.

Not checked:

- A request's own fields, such as `parent` or the resource field in
  `CreateBookRequest`: see
  [`request-required-unenforced`](#protoc-contrib0203request-required-unenforced).
- The fields of a message under a repeated field or a map. A mask cannot
  index into either, so an element is always written whole, and its rules
  hold for create and update alike.
- Another resource the resource holds: it is checked as a resource of its
  own.

A field declared in a file other than the one being linted is reported on the
resource field the path starts from, naming the path.

### `protoc-contrib::0203::request-required-unenforced`

The mirror of
[`resource-required-rejects-zero`](#protoc-contrib0203resource-required-rejects-zero):
a REQUIRED field of an RPC request must carry a `buf.validate` rule that
rejects it when a request leaves it unset.

```proto
message ListItemsRequest {
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    // Either rule alone enforces REQUIRED: "" fails both.
    (buf.validate.field).required = true,
    (buf.validate.field).string.pattern = "^collections/[a-z0-9-]+$"
  ];
}
```

A request's own REQUIRED fields — `parent`, the resource field of a create or
update — have one enforcer: protovalidate, run on every request. A REQUIRED
field no rule enforces is one the schema claims is required and no server
checks.

Which rules reject the unset value is decided exactly as for
`resource-required-rejects-zero`: `required = true`, a minimum length of at
least 1, a pattern `""` does not match, bounds that exclude 0, and the rest
of its table. `IGNORE_ALWAYS` turns every rule off, so a field under it is
reported. A message field is unset only when absent, which only
`required = true` rejects.

A request is the input of a method a service in the file declares, unless it
is a resource. Every message it holds is checked too — singular, repeated or
a map's values — except a resource and what the resource holds, which are
`resource-required-rejects-zero`'s. A field declared in a file other than the
one being linted is reported on the first field of its path in that file, or
else on the method.

### `protoc-contrib::0203::non-empty-default-value`

A field annotated `(google.api.field_behavior) = NON_EMPTY_DEFAULT` must set
`(protoc.contrib.defaults.value)`.

```proto
int32 page_size = 1 [
  (google.api.field_behavior) = OPTIONAL,
  (google.api.field_behavior) = NON_EMPTY_DEFAULT,
  (protoc.contrib.defaults.value).int32 = 50
];
```

[AIP-203](https://google.aip.dev/203) has the service fill in a non-empty
value when a client omits a NON_EMPTY_DEFAULT field. Which value is the
schema's to say, and `(protoc.contrib.defaults.value)` is where
[protoc-gen-go-defaults](https://github.com/protoc-contrib/protoc-gen-go-defaults)
and
[protoc-gen-rust-defaults](https://github.com/protoc-contrib/protoc-gen-rust-defaults)
read it from to generate the code that fills it in. Without it, the
annotation promises a default nothing applies. A schema that does not import
`protoc/contrib/defaults/options.proto` at all is told so. The option is
read by name, so any version of the options file works.

## Configuration

Create an `api-linter` configuration file to enable or disable specific rules:

```yaml
---
- disabled_rules:
    - core::0140::lower-snake
- enabled_rules:
    - core::0123::resource-pattern
```

Pass the config file path as a plugin option:

```bash
protoc \
  --aip-lint_out=. \
  --aip-lint_opt=config=api-linter.yaml \
  proto/example/v1/example.proto
```

See the [api-linter configuration docs](https://linter.aip.dev/configuration)
for the full configuration format.

## CI Integration

Use `output-format=github` with `set-exit-status` to get inline annotations
and fail the build when problems are found:

```yaml
- name: Lint Protos
  run: |
    buf generate --template buf.gen.lint.yaml
```

Where `buf.gen.lint.yaml` contains:

```yaml
version: v2
plugins:
  - local: protoc-gen-aip-lint
    out: .
    opt:
      - output-format=github
      - set-exit-status
```

## Contributing

Contributions are welcome! Please open an issue or pull request.

To set up a development environment with [Nix](https://nixos.org):

```bash
nix develop
go test ./...
```

## License

[MIT](LICENSE)
