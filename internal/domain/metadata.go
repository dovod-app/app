package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A section declares the fields its documents record; an entry carries the
// values. The vocabulary is closed — only a declared key may be written — which
// is what makes a column-per-field view over a section possible, and what lets
// a section that declares nothing behave as it did before the feature existed.
//
// The division of labour, which every later change should be checked against:
// `tags` are free-form and cross-cutting, a metadata field is a typed slot in a
// per-section schema. They answer different questions and neither replaces the
// other.

type FieldType string

const (
	// FieldEnum is a controlled vocabulary. It is the type that actually moves
	// fill rates: a field converted from free text to four to six named options
	// fills several times better, while a typed date or text field fills no
	// better than an untyped one. Types help exactly insofar as they narrow the
	// answer space, so prefer this wherever a field can bear it.
	FieldEnum FieldType = "enum"
	// FieldRef is one or more short codes — E47, R2:E5. Stored as written.
	FieldRef  FieldType = "ref"
	FieldDate FieldType = "date"
	// FieldText is a single line, capped hard. See MaxFieldValueLen.
	FieldText   FieldType = "text"
	FieldNumber FieldType = "number"
	FieldURL    FieldType = "url"
)

// Valid reports whether a caller may declare this type.
//
// The list is closed, and extending it is a code change rather than a
// configuration one, because of the rule that keeps this from becoming a second
// content system: a metadata field may only hold a value you could put in a
// WHERE clause or a facet. No rich text, no long text. If a value needs
// formatting, a paragraph break, or a reference inside prose, it is content and
// belongs in the document.
//
// `boolean` is deliberately absent: absent and false are indistinguishable to a
// reader, so a two-value enum with real words says more.
func (t FieldType) Valid() bool {
	switch t {
	case FieldEnum, FieldRef, FieldDate, FieldText, FieldNumber, FieldURL:
		return true
	}
	return false
}

const (
	// MaxSectionFields protects the completeness signal and the legibility of
	// the block, not the database. Past a dozen the block stops being scannable
	// at the top of a document and nobody reads it — which returns you to the
	// failure this feature exists to fix, where the reader believes the prose
	// instead. Just past the cap people start encoding structure in field names
	// (contact_1, contact_2) and metadata begins carrying content.
	MaxSectionFields = 12
	// MaxRequiredFields is a separate and harder ceiling, because required is
	// the thing that turns a schema edit into non-compliance across every
	// document at once — and because a model, unlike a person, does not leave a
	// field blank when it does not know. It fills it plausibly. Blank used to
	// mean "unknown"; with an agent author there are no blanks.
	MaxRequiredFields = 5
	MaxEnumOptions    = 20
	// MaxFieldValueLen is counted in runes, so a Cyrillic value is not worth
	// half a Latin one.
	MaxFieldValueLen = 200
	MaxFieldLabelLen = 60
	MaxFieldHelpLen  = 200
	MaxFieldKeyLen   = 32
	// MaxImportFileBytes caps one dropped markdown file. Generous next to any
	// document somebody wrote by hand, and small enough that a mis-drop of a
	// database dump is refused before it is read into memory.
	MaxImportFileBytes = 1 << 20
	// MaxRepeatedValues bounds a repeated field so one entry cannot make the
	// section table unreadable on its own.
	MaxRepeatedValues = 20
)

var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// reservedFieldKeys are the keys the Obsidian export already emits as YAML
// front matter. `frontmatter.add` appends blindly and the renderer emits
// everything, so YAML last-wins: a user field keyed `status` would silently
// overwrite the system value in every exported note, and a collision on `code`
// or `aliases` would break every [[E3]] in the vault, since that is what
// Obsidian link resolution hangs on.
//
// The refusal happens when the spec is defined, not when a note is rendered, so
// the error reaches the person who made the mistake instead of surfacing in an
// export months later.
var reservedFieldKeys = map[string]bool{
	"code": true, "title": true, "aliases": true, "research": true,
	"section": true, "type": true, "status": true, "tags": true,
	"created": true, "updated": true, "session": true,
}

// ReservedFieldKeys lists the refused keys, for an error message and for the UI.
func ReservedFieldKeys() []string {
	out := make([]string, 0, len(reservedFieldKeys))
	for k := range reservedFieldKeys {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// FieldSpec is one declared field.
//
// Key and Label are separate on purpose: the key is an immutable identifier and
// the label is what gets renamed. A "rename" that changes the key is a removal
// plus an addition and has to be spelled that way, or the values recorded under
// the old key become unreachable without anyone deciding that they should.
type FieldSpec struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required"`
	Repeated bool      `json:"repeated,omitempty"`
	// Options is the vocabulary for FieldEnum, ignored otherwise.
	Options []string `json:"options,omitempty"`
	// Help says where the value comes from — "the service name from the repo;
	// ask if unclear". The agent reads it, which is the whole reason it exists:
	// a required field with no provenance note is an invitation to invent.
	Help string `json:"help,omitempty"`
}

// ValidateFieldSpecs checks a section's whole declaration and returns one
// message per problem. It returns errors rather than a single failure because
// the caller is editing a form and deserves to see every mistake at once.
func ValidateFieldSpecs(specs []FieldSpec) []string {
	var errs []string
	if len(specs) > MaxSectionFields {
		errs = append(errs, fmt.Sprintf("a section may declare at most %d fields, got %d", MaxSectionFields, len(specs)))
	}

	seen := map[string]bool{}
	required := 0
	for i, f := range specs {
		where := fmt.Sprintf("fields[%d]", i)

		switch {
		case f.Key == "":
			errs = append(errs, where+": key is required")
		case !fieldKeyPattern.MatchString(f.Key):
			errs = append(errs, fmt.Sprintf("%s: key %q must be lowercase, start with a letter, and hold only letters, digits and underscores", where, f.Key))
		case utf8.RuneCountInString(f.Key) > MaxFieldKeyLen:
			errs = append(errs, fmt.Sprintf("%s: key %q is longer than %d characters", where, f.Key, MaxFieldKeyLen))
		case reservedFieldKeys[f.Key]:
			errs = append(errs, fmt.Sprintf("%s: key %q is reserved — the export already emits it as front matter, and a second one would overwrite it", where, f.Key))
		case seen[f.Key]:
			errs = append(errs, fmt.Sprintf("%s: key %q is declared twice", where, f.Key))
		default:
			seen[f.Key] = true
		}

		if !f.Type.Valid() {
			errs = append(errs, fmt.Sprintf("%s: unknown type %q", where, f.Type))
		}
		if utf8.RuneCountInString(f.Label) > MaxFieldLabelLen {
			errs = append(errs, fmt.Sprintf("%s: label is longer than %d characters", where, MaxFieldLabelLen))
		}
		if utf8.RuneCountInString(f.Help) > MaxFieldHelpLen {
			errs = append(errs, fmt.Sprintf("%s: help is longer than %d characters", where, MaxFieldHelpLen))
		}

		if f.Type == FieldEnum {
			if len(f.Options) == 0 {
				errs = append(errs, where+": an enum field needs at least one option")
			}
			if len(f.Options) > MaxEnumOptions {
				errs = append(errs, fmt.Sprintf("%s: at most %d options, got %d", where, MaxEnumOptions, len(f.Options)))
			}
			opts := map[string]bool{}
			for _, o := range f.Options {
				if strings.TrimSpace(o) == "" {
					errs = append(errs, where+": an option may not be blank")
					continue
				}
				if opts[o] {
					errs = append(errs, fmt.Sprintf("%s: option %q is listed twice", where, o))
				}
				opts[o] = true
			}
		} else if len(f.Options) > 0 {
			errs = append(errs, fmt.Sprintf("%s: only an enum field takes options", where))
		}

		if f.Required {
			required++
		}
	}

	if required > MaxRequiredFields {
		errs = append(errs, fmt.Sprintf("at most %d fields may be required, got %d — a required field a writer cannot answer gets invented rather than left blank", MaxRequiredFields, required))
	}
	return errs
}

// MetadataIssue names one key the write could not honour as given.
type MetadataIssue struct {
	Key string `json:"key"`
	// Value is what the writer actually sent, when a caller fills it in.
	//
	// Set by the markdown import and by nothing else. An agent that sent a bad
	// value still has it; a person looking at a file they did not write does
	// not, and "stage is not one of the options" without saying what the file
	// said is a report they have to go and check by hand.
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason"`
}

// MetadataReport is what a write tells its author about the metadata it was
// handed. It is attached to a response and never stored — the same shape and
// the same reason as BlockSaveReport: an author whose value was dropped has to
// be told, because the loss is otherwise silent.
//
// A write is never refused because of metadata. The author is a model in the
// middle of an interview, and a rejection there destroys the answers a person
// has already given.
type MetadataReport struct {
	Stored []string `json:"stored,omitempty"`
	// UnknownKeys were not declared by this section and are dropped. This is
	// what "closed vocabulary" means in practice.
	UnknownKeys []MetadataIssue `json:"unknown_keys,omitempty"`
	// InvalidValues were declared but did not match their type. They are stored
	// verbatim and flagged: discarding a person's value to protect a type is the
	// same mistake as refusing the write.
	InvalidValues   []MetadataIssue `json:"invalid_values,omitempty"`
	MissingRequired []string        `json:"missing_required,omitempty"`
	SpecVersion     int             `json:"spec_version"`
}

// Complete reports whether every required field carries a value.
func (r MetadataReport) Complete() bool { return len(r.MissingRequired) == 0 }

// ValidateMetadata checks submitted values against a section's declaration and
// returns what to store alongside a report of everything it could not honour.
//
// It never returns an error: nothing here is worth failing a write over.
func ValidateMetadata(specs []FieldSpec, values map[string]any) (map[string]any, MetadataReport) {
	stored := map[string]any{}
	var report MetadataReport

	byKey := map[string]FieldSpec{}
	order := make([]string, 0, len(specs))
	for _, f := range specs {
		byKey[f.Key] = f
		order = append(order, f.Key)
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sortStrings(keys)

	for _, k := range keys {
		v := values[k]
		spec, declared := byKey[k]
		if !declared {
			report.UnknownKeys = append(report.UnknownKeys, MetadataIssue{
				Key:    k,
				Reason: "this section does not declare a field with this key",
			})
			continue
		}
		if v == nil {
			// A null is an explicit "we do not know", and it is the answer to a
			// required field that nobody can honestly fill. Keeping it separable
			// from an absent key is the whole point: a model, unlike a person,
			// does not leave a field blank when it does not know — it fills it
			// plausibly — so the writable unknown is what stops a required field
			// from manufacturing a fact.
			stored[k] = nil
			report.Stored = append(report.Stored, k)
			continue
		}
		if isBlankValue(v) {
			// An empty string clears the field rather than storing something that
			// would read as an answer.
			continue
		}
		normalized, err := normalizeValue(spec, v)
		if err != nil {
			// Stored anyway, and flagged.
			stored[k] = v
			report.InvalidValues = append(report.InvalidValues, MetadataIssue{Key: k, Reason: err.Error()})
			report.Stored = append(report.Stored, k)
			continue
		}
		stored[k] = normalized
		report.Stored = append(report.Stored, k)
	}

	for _, k := range order {
		f := byKey[k]
		if f.Required {
			if _, ok := stored[k]; !ok {
				report.MissingRequired = append(report.MissingRequired, k)
			}
		}
	}
	return stored, report
}

// OrphanedKeys are values a document still carries under keys the section no
// longer declares. They are never deleted — removing a field is a decision
// about future collection, not a verdict on what was already written — so the
// UI shows them apart rather than pretending they are gone.
func OrphanedKeys(specs []FieldSpec, values map[string]any) []string {
	declared := map[string]bool{}
	for _, f := range specs {
		declared[f.Key] = true
	}
	var out []string
	for k := range values {
		if !declared[k] {
			out = append(out, k)
		}
	}
	sortStrings(out)
	return out
}

// MissingRequired lists required keys a stored document does not answer. It is
// computed on read against the current spec and never persisted: a stored
// completeness flag goes stale the moment the declaration changes.
func MissingRequired(specs []FieldSpec, values map[string]any) []string {
	var out []string
	for _, f := range specs {
		if !f.Required {
			continue
		}
		v, ok := values[f.Key]
		if !ok {
			out = append(out, f.Key)
			continue
		}
		// An explicit unknown answers the requirement. It says somebody looked.
		if v != nil && isBlankValue(v) {
			out = append(out, f.Key)
		}
	}
	return out
}

func isBlankValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	}
	return false
}

// normalizeValue coerces one submitted value into the shape its type promises,
// or explains why it cannot.
func normalizeValue(spec FieldSpec, v any) (any, error) {
	if spec.Repeated {
		items, err := toList(v)
		if err != nil {
			return nil, err
		}
		if len(items) > MaxRepeatedValues {
			return nil, fmt.Errorf("at most %d values, got %d", MaxRepeatedValues, len(items))
		}
		out := make([]any, 0, len(items))
		var firstErr error
		for _, item := range items {
			nv, err := normalizeScalar(spec, item)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			out = append(out, nv)
		}
		return out, firstErr
	}
	if _, isList := v.([]any); isList {
		return nil, fmt.Errorf("field %q takes a single value, not a list", spec.Key)
	}
	return normalizeScalar(spec, v)
}

func toList(v any) ([]any, error) {
	switch t := v.(type) {
	case []any:
		return t, nil
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out, nil
	default:
		// A single value for a repeated field is a courtesy, not an error: an
		// author writing one service name should not have to know it is a list.
		return []any{v}, nil
	}
}

func normalizeScalar(spec FieldSpec, v any) (any, error) {
	switch spec.Type {
	case FieldNumber:
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil {
				return v, fmt.Errorf("%q is not a number", n)
			}
			return f, nil
		default:
			return v, fmt.Errorf("expected a number")
		}
	}

	s, ok := v.(string)
	if !ok {
		return v, fmt.Errorf("expected text")
	}
	s = strings.TrimSpace(oneLineValue(s))
	if utf8.RuneCountInString(s) > MaxFieldValueLen {
		return v, fmt.Errorf("longer than %d characters — a value that needs more room is content, and belongs in the document", MaxFieldValueLen)
	}

	switch spec.Type {
	case FieldEnum:
		for _, o := range spec.Options {
			if o == s {
				return s, nil
			}
		}
		return s, fmt.Errorf("%q is not one of: %s", s, strings.Join(spec.Options, ", "))
	case FieldDate:
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return s, nil
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
		return s, fmt.Errorf("%q is not a date — expected YYYY-MM-DD", s)
	case FieldRef:
		ref := strings.TrimSpace(strings.Trim(s, "[]"))
		if !shortCodeRef.MatchString(ref) {
			return s, fmt.Errorf("%q is not a short code — expected E12, R2:E5 or RM1", s)
		}
		return ref, nil
	case FieldURL:
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return s, fmt.Errorf("%q is not an http or https URL", s)
		}
		return s, nil
	}
	return s, nil
}

// shortCodeRef matches the reference shapes this product already understands.
var shortCodeRef = regexp.MustCompile(`^(?:R\d+:)?(?:E|S|SS|Q|T|RM|N)\d+(?::N\d+)?$|^R\d+$`)

// oneLineValue flattens newlines: a metadata value is a cell, and a value
// carrying a paragraph break is content wearing a field's clothes.
func oneLineValue(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

// sortStrings keeps the domain package free of a sort import in one place only.
// sortStrings was an insertion sort, which is Θ(n²) on the input it actually
// gets: ValidateMetadata builds its key slice by ranging a map, so every call
// hands it a fresh random permutation. That was invisible while the only writer
// was an agent sending a dozen declared keys. A markdown file can carry a
// hundred thousand of them under the one-megabyte cap, and the preview route
// runs the validator before anything is refused — measured at over two minutes
// of CPU for a 1 MB file, from one request, on the caller's own section.
func sortStrings(s []string) {
	sort.Strings(s)
}

// MetadataIssues re-checks stored values against the declaration, so a reader
// can be told that `stage: "sent for review"` is not one of the options.
//
// It exists because MetadataReport is attached to a write and never stored: a
// value that was invalid on Tuesday is still invalid on Friday, and the person
// opening the document on Friday is the one who can fix it. Without this the
// client would have to re-implement enum membership, date parsing, URL schemes
// and the length cap as a second source of truth.
func MetadataIssues(specs []FieldSpec, values map[string]any) []MetadataIssue {
	var out []MetadataIssue
	for _, f := range specs {
		v, ok := values[f.Key]
		if !ok || v == nil {
			continue
		}
		if _, err := normalizeValue(f, v); err != nil {
			out = append(out, MetadataIssue{Key: f.Key, Reason: err.Error()})
		}
	}
	return out
}

// FieldTypeInfo describes one type to a client, so the editor does not carry a
// second copy of the catalogue that drifts on the seventh type.
type FieldTypeInfo struct {
	Type        FieldType `json:"type"`
	NeedsOption bool      `json:"needs_options"`
	Repeatable  bool      `json:"repeatable"`
	Hint        string    `json:"hint"`
}

// FieldSchemaInfo is everything a client needs to render a spec editor without
// hard-coding this package's rules.
type FieldSchemaInfo struct {
	Types        []FieldTypeInfo `json:"types"`
	ReservedKeys []string        `json:"reserved_keys"`
	Caps         map[string]any  `json:"caps"`
}

// FieldSchema serves the rules rather than restating them in the frontend. A
// cap the client believes and the server enforces will disagree exactly once,
// at the worst moment.
func FieldSchema() FieldSchemaInfo {
	return FieldSchemaInfo{
		Types: []FieldTypeInfo{
			{Type: FieldEnum, NeedsOption: true, Repeatable: true, Hint: "A short list of named options. The type that actually raises how often a field gets filled — prefer it wherever the answer can be enumerated."},
			{Type: FieldRef, Repeatable: true, Hint: "A short code of something in this product: E12, R2:E5, RM1."},
			{Type: FieldDate, Repeatable: false, Hint: "A calendar date, YYYY-MM-DD."},
			{Type: FieldText, Repeatable: true, Hint: "One short line. Anything needing a paragraph belongs in the document."},
			{Type: FieldNumber, Repeatable: false, Hint: "A number."},
			{Type: FieldURL, Repeatable: true, Hint: "An http or https address."},
		},
		ReservedKeys: ReservedFieldKeys(),
		Caps: map[string]any{
			"fields":      MaxSectionFields,
			"required":    MaxRequiredFields,
			"options":     MaxEnumOptions,
			"values":      MaxRepeatedValues,
			"text_max":    MaxFieldValueLen,
			"label_max":   MaxFieldLabelLen,
			"help_max":    MaxFieldHelpLen,
			"key_max":     MaxFieldKeyLen,
			"key_pattern": fieldKeyPattern.String(),
			// Import's caps ride here rather than in a payload of their own:
			// the drop hint, the refusal and the title counter all quote one of
			// them, and a number hard-coded in the frontend is a lie the day it
			// changes on the server.
			"import_max_bytes":  MaxImportFileBytes,
			"import_extensions": []string{".md", ".markdown"},
			// The section's writing instruction is edited on the same settings
			// card as the field spec, beside the fields it is supposed to name,
			// and its counter has to quote the number the server refuses on.
			"instruction_max": SectionInstructionMax,
		},
	}
}
