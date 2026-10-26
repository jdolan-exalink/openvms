package camconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// This file is the engine-side half of "Avanzado → Editar YAML" (PRD §34): parse the YAML of
// one camera section, check it against the engine's schema, diff it against the current
// section and find which parts OpenVMS manages. It is pure: nothing here reads or writes an
// engine, the inventory service does that and applies the result through the existing
// sectioned PATCH.

// SyntaxIssue is a YAML syntax error with its position (1-based). Column is the first
// non-blank column of the line when the parser reports only the line.
type SyntaxIssue struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// SchemaIssue is a value the engine's schema does not accept; Path is dotted from the section.
type SchemaIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Leaf change kinds.
const (
	LeafAdded   = "added"
	LeafRemoved = "removed"
	LeafChanged = "changed"
)

// LeafChange is one value that differs between the current section and the edited YAML.
type LeafChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// ManagedPath is a place in the engine's configuration that OpenVMS owns because its effective
// desired state (profile overlaid with the camera's overrides) sets it.
type ManagedPath struct {
	// PatchPath is the normalized-model path (detection.enabled) and EnginePath the dotted
	// path in the camera's engine document (detect.enabled), always inside one section.
	PatchPath  string `json:"patch_path"`
	EnginePath string `json:"engine_path"`
	Value      any    `json:"value"`
}

// ManagedConflict is an edit that changes a managed path to something other than what OpenVMS
// wants: applying it would put the camera out of step with its desired state.
type ManagedConflict struct {
	PatchPath  string `json:"patch_path"`
	EnginePath string `json:"engine_path"`
	Desired    any    `json:"desired"`
	New        any    `json:"new"`
}

var (
	reYAMLLine = regexp.MustCompile(`line (\d+): ?(.*)`)
	reYAMLCol  = regexp.MustCompile(`column (\d+)`)
)

// ParseSectionYAML decodes the YAML of one section into JSON-compatible values (maps with
// string keys, lists, strings, float64, bool, nil). The text must hold exactly one document.
// Syntax problems come back as issues with line and column, never as a Go error.
func ParseSectionYAML(text string) (any, []SyntaxIssue) {
	if strings.TrimSpace(text) == "" {
		return nil, []SyntaxIssue{{Line: 1, Column: 1, Message: "el YAML está vacío"}}
	}
	if iss := tabIndentIssue(text); iss != nil {
		return nil, []SyntaxIssue{*iss}
	}
	dec := yaml.NewDecoder(strings.NewReader(text))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, []SyntaxIssue{syntaxIssue(text, err)}
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		line := 1 + strings.Count(strings.TrimRight(text, "\n"), "\n")
		return nil, []SyntaxIssue{{Line: line, Column: 1, Message: "el YAML debe tener un solo documento"}}
	}
	out, err := canonical(v)
	if err != nil {
		return nil, []SyntaxIssue{{Line: 1, Column: 1, Message: err.Error()}}
	}
	return out, nil
}

// tabIndentIssue finds a tab used as indentation, which YAML forbids.
func tabIndentIssue(text string) *SyntaxIssue {
	for i, line := range strings.Split(text, "\n") {
		trim := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trim, "\t") {
			return &SyntaxIssue{Line: i + 1, Column: len(line) - len(trim) + 1, Message: "la sangría no puede usar tabuladores; use espacios"}
		}
	}
	return nil
}

func syntaxIssue(text string, err error) SyntaxIssue {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.TrimPrefix(msg, "unmarshal errors:\n  ")
	iss := SyntaxIssue{Line: 1, Column: 1, Message: strings.TrimSpace(msg)}
	if m := reYAMLLine.FindStringSubmatch(msg); m != nil {
		iss.Line, _ = strconv.Atoi(m[1])
		iss.Message = strings.TrimSpace(m[2])
		if iss.Message == "" {
			iss.Message = strings.TrimSpace(msg)
		}
		if c := reYAMLCol.FindStringSubmatch(msg); c != nil {
			iss.Column, _ = strconv.Atoi(c[1])
		} else if lines := strings.Split(text, "\n"); iss.Line >= 1 && iss.Line <= len(lines) {
			l := lines[iss.Line-1]
			iss.Column = len(l) - len(strings.TrimLeft(l, " ")) + 1
		}
	}
	return iss
}

// canonical converts decoded YAML to the JSON data model so it compares with the engine's
// JSON document: string keys only, numbers as float64, timestamps as text.
func canonical(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case uint64:
		return float64(t), nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, errors.New("los valores NaN e infinito no son válidos")
		}
		return t, nil
	case time.Time:
		return t.Format(time.RFC3339), nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			c, err := canonical(e)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			c, err := canonical(e)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("las claves deben ser texto (se encontró %v)", k)
			}
			c, err := canonical(e)
			if err != nil {
				return nil, err
			}
			out[ks] = c
		}
		return out, nil
	}
	return nil, fmt.Errorf("tipo de valor no admitido: %T", v)
}

// Canonical normalizes a JSON-decoded or Go value to the model ParseSectionYAML produces.
func Canonical(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// SectionYAML renders a section value as YAML (2-space indent, sorted keys). A map renders as
// its contents, so the text is what goes under `detect:` in config.yml, not the whole key.
func SectionYAML(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// --- schema --------------------------------------------------------------------------------

// ErrUnknownSection is a section the engine's schema does not define for a camera.
var ErrUnknownSection = errors.New("camconfig: unknown camera section")

const maxSchemaIssues = 50

// CheckSection validates value against the section's definition in the engine's camera
// schema (Frigate's CameraConfig JSON schema, as served by the schema endpoint). It is a
// conservative structural check: types, enums, ranges, lengths, required and unknown keys. A
// construct it does not understand is accepted, so the engine stays the final judge.
func CheckSection(schema json.RawMessage, section string, value any) ([]SchemaIssue, error) {
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, fmt.Errorf("camconfig: schema: %w", err)
	}
	defs, _ := root["$defs"].(map[string]any)
	c := &schemaChecker{defs: defs}
	cam, ok := c.resolve(root).(map[string]any)
	if !ok {
		return nil, errors.New("camconfig: schema has no camera definition")
	}
	props, _ := cam["properties"].(map[string]any)
	sec, ok := props[section]
	if !ok {
		return nil, ErrUnknownSection
	}
	c.walk("", sec, value, 0)
	return c.issues, nil
}

// SectionNames lists the camera sections the schema defines, sorted.
func SectionNames(schema json.RawMessage) []string {
	var root map[string]any
	if json.Unmarshal(schema, &root) != nil {
		return nil
	}
	defs, _ := root["$defs"].(map[string]any)
	c := &schemaChecker{defs: defs}
	cam, _ := c.resolve(root).(map[string]any)
	props, _ := cam["properties"].(map[string]any)
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type schemaChecker struct {
	defs   map[string]any
	issues []SchemaIssue
}

func (c *schemaChecker) resolve(s any) any {
	for i := 0; i < 8; i++ {
		m, ok := s.(map[string]any)
		if !ok {
			return s
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return s
		}
		name, found := strings.CutPrefix(ref, "#/$defs/")
		if !found {
			return s
		}
		d, ok := c.defs[name]
		if !ok {
			return map[string]any{}
		}
		s = d
	}
	return s
}

func (c *schemaChecker) add(path, format string, args ...any) {
	if len(c.issues) < maxSchemaIssues {
		c.issues = append(c.issues, SchemaIssue{Path: path, Message: fmt.Sprintf(format, args...)})
	}
}

func kindOf(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if t == math.Trunc(t) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

var kindLabel = map[string]string{
	"null": "vacío", "boolean": "verdadero/falso", "string": "texto", "integer": "número entero",
	"number": "número", "array": "lista", "object": "objeto",
}

func label(k string) string {
	if l, ok := kindLabel[k]; ok {
		return l
	}
	return k
}

func typeMatches(want, got string) bool {
	return want == got || (want == "number" && got == "integer")
}

func (c *schemaChecker) walk(path string, s any, v any, depth int) {
	if depth > 24 || len(c.issues) >= maxSchemaIssues {
		return
	}
	s = c.resolve(s)
	m, ok := s.(map[string]any)
	if !ok {
		return
	}
	if alts := firstList(m, "anyOf", "oneOf"); alts != nil {
		c.walkAlternatives(path, alts, v, depth)
	}
	if all, ok := m["allOf"].([]any); ok {
		for _, a := range all {
			c.walk(path, a, v, depth+1)
		}
	}
	if !c.checkType(path, m, v) {
		return
	}
	if en, ok := m["enum"].([]any); ok && !containsValue(en, v) {
		c.add(path, "valor no permitido (opciones: %s)", joinValues(en))
		return
	}
	if cv, ok := m["const"]; ok && !reflect.DeepEqual(cv, v) {
		c.add(path, "el valor debe ser %v", cv)
		return
	}
	switch t := v.(type) {
	case map[string]any:
		c.walkObject(path, m, t, depth)
	case []any:
		c.walkArray(path, m, t, depth)
	case string:
		c.walkString(path, m, t)
	case float64:
		c.walkNumber(path, m, t)
	}
}

func firstList(m map[string]any, keys ...string) []any {
	for _, k := range keys {
		if l, ok := m[k].([]any); ok && len(l) > 0 {
			return l
		}
	}
	return nil
}

// walkAlternatives accepts the value when any alternative does; otherwise it reports the
// issues of the alternative whose type matches (an Optional[X] is X or null).
func (c *schemaChecker) walkAlternatives(path string, alts []any, v any, depth int) {
	got := kindOf(v)
	var typed []any
	for _, a := range alts {
		sub := &schemaChecker{defs: c.defs}
		sub.walk(path, a, v, depth+1)
		if len(sub.issues) == 0 {
			return
		}
		if am, ok := c.resolve(a).(map[string]any); ok && declaresType(am, got) {
			typed = append(typed, a)
		}
	}
	if len(typed) == 1 {
		c.walk(path, typed[0], v, depth+1)
		return
	}
	if want := alternativeTypes(c, alts); len(want) > 0 {
		c.add(path, "se esperaba %s y se encontró %s", strings.Join(want, " o "), label(got))
		return
	}
	c.add(path, "el valor no coincide con ningún formato permitido")
}

// alternativeTypes lists the plain types of the alternatives, or nil when any alternative is
// not a plain typed one.
func alternativeTypes(c *schemaChecker, alts []any) []string {
	var out []string
	for _, a := range alts {
		am, ok := c.resolve(a).(map[string]any)
		if !ok {
			return nil
		}
		t, ok := am["type"].(string)
		if !ok {
			return nil
		}
		out = append(out, label(t))
	}
	return out
}

func declaresType(m map[string]any, got string) bool {
	switch t := m["type"].(type) {
	case string:
		return typeMatches(t, got)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && typeMatches(s, got) {
				return true
			}
		}
	}
	return false
}

func (c *schemaChecker) checkType(path string, m map[string]any, v any) bool {
	t, ok := m["type"]
	if !ok {
		return true
	}
	got := kindOf(v)
	var want []string
	switch tt := t.(type) {
	case string:
		want = []string{tt}
	case []any:
		for _, e := range tt {
			if s, ok := e.(string); ok {
				want = append(want, s)
			}
		}
	}
	for _, w := range want {
		if typeMatches(w, got) {
			return true
		}
	}
	if len(want) == 0 {
		return true
	}
	labels := make([]string, len(want))
	for i, w := range want {
		labels[i] = label(w)
	}
	c.add(path, "se esperaba %s y se encontró %s", strings.Join(labels, " o "), label(got))
	return false
}

func (c *schemaChecker) walkObject(path string, m map[string]any, v map[string]any, depth int) {
	props, _ := m["properties"].(map[string]any)
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if k, ok := r.(string); ok {
				if _, has := v[k]; !has {
					c.add(join(path, k), "falta el campo obligatorio")
				}
			}
		}
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p, ok := props[k]; ok {
			c.walk(join(path, k), p, v[k], depth+1)
			continue
		}
		switch ap := m["additionalProperties"].(type) {
		case bool:
			if !ap {
				c.add(join(path, k), "clave desconocida")
			}
		case map[string]any:
			c.walk(join(path, k), ap, v[k], depth+1)
		}
	}
}

func (c *schemaChecker) walkArray(path string, m map[string]any, v []any, depth int) {
	if n, ok := num(m["minItems"]); ok && float64(len(v)) < n {
		c.add(path, "se necesitan al menos %d elementos", int(n))
	}
	if n, ok := num(m["maxItems"]); ok && float64(len(v)) > n {
		c.add(path, "se admiten como máximo %d elementos", int(n))
	}
	if it, ok := m["items"]; ok {
		for i, e := range v {
			c.walk(join(path, strconv.Itoa(i)), it, e, depth+1)
		}
	}
}

func (c *schemaChecker) walkString(path string, m map[string]any, v string) {
	if n, ok := num(m["minLength"]); ok && float64(len([]rune(v))) < n {
		c.add(path, "el texto es demasiado corto (mínimo %d caracteres)", int(n))
	}
	if n, ok := num(m["maxLength"]); ok && float64(len([]rune(v))) > n {
		c.add(path, "el texto es demasiado largo (máximo %d caracteres)", int(n))
	}
	if p, ok := m["pattern"].(string); ok {
		if re, err := regexp.Compile(p); err == nil && !re.MatchString(v) {
			c.add(path, "el texto no tiene el formato esperado")
		}
	}
}

func (c *schemaChecker) walkNumber(path string, m map[string]any, v float64) {
	if n, ok := num(m["minimum"]); ok && v < n {
		c.add(path, "el valor debe ser como mínimo %v", n)
	}
	if n, ok := num(m["maximum"]); ok && v > n {
		c.add(path, "el valor debe ser como máximo %v", n)
	}
	if n, ok := num(m["exclusiveMinimum"]); ok && v <= n {
		c.add(path, "el valor debe ser mayor que %v", n)
	}
	if n, ok := num(m["exclusiveMaximum"]); ok && v >= n {
		c.add(path, "el valor debe ser menor que %v", n)
	}
}

func num(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func containsValue(list []any, v any) bool {
	for _, e := range list {
		if reflect.DeepEqual(e, v) {
			return true
		}
	}
	return false
}

func joinValues(list []any) string {
	parts := make([]string, 0, len(list))
	for _, e := range list {
		parts = append(parts, fmt.Sprint(e))
	}
	return strings.Join(parts, ", ")
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

// --- diff ----------------------------------------------------------------------------------

// DiffLeaves lists the values that differ between before and after, both in the model
// ParseSectionYAML produces. Maps are compared key by key; a list or scalar is one value.
// Paths are dotted from the section, sorted.
func DiffLeaves(before, after any) []LeafChange {
	var out []LeafChange
	diffInto(&out, "", before, after)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func diffInto(out *[]LeafChange, path string, before, after any) {
	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	switch {
	case bok && aok:
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		for k := range keys {
			b, bhas := bm[k]
			a, ahas := am[k]
			switch {
			case bhas && ahas:
				diffInto(out, join(path, k), b, a)
			case ahas:
				flatten(out, join(path, k), a, LeafAdded)
			default:
				flatten(out, join(path, k), b, LeafRemoved)
			}
		}
	case bok:
		flatten(out, path, before, LeafRemoved)
		flatten(out, path, after, LeafAdded)
	case aok:
		flatten(out, path, before, LeafRemoved)
		flatten(out, path, after, LeafAdded)
	case !reflect.DeepEqual(before, after):
		*out = append(*out, LeafChange{Path: path, Kind: LeafChanged, From: before, To: after})
	}
}

func flatten(out *[]LeafChange, path string, v any, kind string) {
	if m, ok := v.(map[string]any); ok && len(m) > 0 {
		for k, e := range m {
			flatten(out, join(path, k), e, kind)
		}
		return
	}
	lc := LeafChange{Path: path, Kind: kind}
	if kind == LeafAdded {
		lc.To = v
	} else {
		lc.From = v
	}
	*out = append(*out, lc)
}

var (
	reURLUser   = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]*@`)
	reSecretKey = regexp.MustCompile(`(?i)(^|\.)(pass(word)?|pwd|user(name)?|token|secret|key|authorization)$`)
)

const hidden = "[oculto]"

// SafeValue removes credentials from a value shown in a diff or stored in the audit trail:
// the user info of URLs and the value of any key named like a secret.
func SafeValue(path string, v any) any {
	if reSecretKey.MatchString(path) {
		if v == nil {
			return nil
		}
		return hidden
	}
	switch t := v.(type) {
	case string:
		return reURLUser.ReplaceAllString(t, "${1}")
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = SafeValue(path, e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = SafeValue(join(path, k), e)
		}
		return out
	}
	return v
}

// SafeChanges returns the changes with credentials removed (see SafeValue).
func SafeChanges(changes []LeafChange) []LeafChange {
	out := make([]LeafChange, len(changes))
	for i, c := range changes {
		out[i] = LeafChange{Path: c.Path, Kind: c.Kind, From: SafeValue(c.Path, c.From), To: SafeValue(c.Path, c.To)}
	}
	return out
}

// --- redacted placeholders -----------------------------------------------------------------

// ResolvePlaceholders prepares the edited section for writing. The person edited the redacted
// view (shown); a placeholder they left untouched must not overwrite the actual secret, so
// where actual (the unredacted section, nil when unavailable) holds the value it is put back,
// and otherwise the key is dropped from maps (the engine merges, so the stored value stays)
// and refused inside lists (a list is replaced as a whole). A placeholder that does not equal
// the one that was shown is a new, unusable value and is refused.
func ResolvePlaceholders(edited, shown, actual any) (any, error) {
	out, err := resolve("", edited, shown, actual, false)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var errDrop = errors.New("drop")

func resolve(path string, edited, shown, actual any, inList bool) (any, error) {
	switch t := edited.(type) {
	case string:
		if !frigate.IsRedacted(t) {
			return t, nil
		}
		if s, _ := shown.(string); s != t {
			return nil, invalid("%s: el valor oculto no se puede escribir; deje el que se mostró o escriba el valor real", pathLabel(path))
		}
		if r, ok := actual.(string); ok && !frigate.IsRedacted(r) {
			return r, nil
		}
		return nil, errDrop
	case map[string]any:
		sm, _ := shown.(map[string]any)
		am, _ := actual.(map[string]any)
		out := make(map[string]any, len(t))
		for k, e := range t {
			v, err := resolve(join(path, k), e, sm[k], am[k], inList)
			if errors.Is(err, errDrop) {
				if inList {
					return nil, errListSecret(path, k)
				}
				continue
			}
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case []any:
		sl, _ := shown.([]any)
		rl, _ := actual.([]any)
		out := make([]any, len(t))
		for i, e := range t {
			var s, r any
			if i < len(sl) {
				s = sl[i]
			}
			if i < len(rl) {
				r = rl[i]
			}
			v, err := resolve(join(path, strconv.Itoa(i)), e, s, r, true)
			if errors.Is(err, errDrop) {
				return nil, errListSecret(path, "")
			}
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	return edited, nil
}

func errListSecret(path, key string) error {
	return invalid("%s: contiene un valor oculto que no se puede conservar dentro de una lista; edítela con el permiso de credenciales", pathLabel(join(path, key)))
}

func pathLabel(p string) string {
	if p == "" {
		return "la sección"
	}
	return p
}

// PruneUnchanged returns edited without the parts equal to current: maps are pruned key by
// key (the engine merges them), lists and scalars are kept only when they differ. nil means
// nothing changes. Keys present in current but missing from edited are not deletions: the
// engine cannot remove a key through a merge.
func PruneUnchanged(edited, current any) any {
	em, eok := edited.(map[string]any)
	cm, cok := current.(map[string]any)
	if eok && cok {
		out := map[string]any{}
		for k, e := range em {
			c, has := cm[k]
			if !has {
				out[k] = e
				continue
			}
			if p := PruneUnchanged(e, c); p != nil {
				out[k] = p
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	if reflect.DeepEqual(edited, current) {
		return nil
	}
	if edited == nil {
		return nil
	}
	return edited
}

// --- managed paths -------------------------------------------------------------------------

// ManagedPaths lists the engine paths OpenVMS owns through its effective desired state
// (profile overlaid with the camera's overrides), with the value it wants. Only paths the
// Frigate adapter writes appear; zones are handled by ManagedZonePaths.
func ManagedPaths(desired Patch) []ManagedPath {
	d := desired.Normalized()
	var out []ManagedPath
	add := func(patch, engine string, v any) {
		out = append(out, ManagedPath{PatchPath: patch, EnginePath: engine, Value: v})
	}
	if d.Detection != nil {
		if d.Detection.Enabled != nil {
			add("detection.enabled", "detect.enabled", *d.Detection.Enabled)
		}
		if d.Detection.Objects != nil {
			add("detection.objects", "objects.track", anyStrings(*d.Detection.Objects))
		}
	}
	if d.Recording != nil && d.Recording.Enabled != nil {
		add("recording.enabled", "record.enabled", *d.Recording.Enabled)
	}
	if a := d.Analytics; a != nil {
		if a.LPR != nil {
			add("analytics.lpr", "lpr.enabled", *a.LPR)
		}
		for _, id := range sortedModuleIDs(a.Modules) {
			sec, ok := moduleSections[id]
			mp := a.Modules[id]
			if !ok || mp == nil {
				continue
			}
			if mp.Enabled != nil && id != ModuleLPR {
				add("analytics.modules."+id+".enabled", sec+".enabled", *mp.Enabled)
			}
			if mp.Settings != nil && mp.Settings.MinArea != nil {
				add("analytics.modules."+id+".settings.min_area", sec+".min_area", float64(*mp.Settings.MinArea))
			}
		}
	}
	if s := d.Streams; s != nil && s.Detection != nil {
		if r := s.Detection.Resolution; r != nil {
			add("streams.detection.resolution", "detect.width", float64(r.Width))
			add("streams.detection.resolution", "detect.height", float64(r.Height))
		}
		if f := s.Detection.FPS; f != nil {
			add("streams.detection.fps", "detect.fps", *f)
		}
	}
	return out
}

func anyStrings(s []string) []any {
	out := make([]any, len(s))
	for i, e := range s {
		out[i] = e
	}
	return out
}

// ManagedZonePrefix marks the zones OpenVMS owns in the engine.
const ManagedZonePrefix = "ovms_"

// ManagedZonePaths lists the OpenVMS-owned zones present in the camera's zones section.
func ManagedZonePaths(zones any) []ManagedPath {
	m, _ := zones.(map[string]any)
	var out []ManagedPath
	for _, k := range sortedKeys(m) {
		if strings.HasPrefix(k, ManagedZonePrefix) {
			out = append(out, ManagedPath{PatchPath: "zones." + k, EnginePath: "zones." + k, Value: m[k]})
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// InSection keeps the managed paths that fall inside one engine section and rewrites their
// engine path relative to it (detect.enabled -> enabled), the form diffs use.
func InSection(paths []ManagedPath, section string) []ManagedPath {
	var out []ManagedPath
	for _, p := range paths {
		rel, ok := strings.CutPrefix(p.EnginePath, section+".")
		if !ok {
			continue
		}
		p.EnginePath = rel
		out = append(out, p)
	}
	return out
}

// ManagedConflicts returns the managed paths the changes would move away from the value
// OpenVMS wants. A change that sets exactly the desired value is no conflict. Paths are
// relative to the section, like the changes.
func ManagedConflicts(managed []ManagedPath, changes []LeafChange) []ManagedConflict {
	var out []ManagedConflict
	seen := map[string]bool{}
	for _, m := range managed {
		for _, c := range changes {
			if !pathsOverlap(m.EnginePath, c.Path) {
				continue
			}
			var nv any
			if c.Kind != LeafRemoved {
				nv = c.To
			}
			if c.Path == m.EnginePath && c.Kind != LeafRemoved && reflect.DeepEqual(nv, m.Value) {
				continue
			}
			key := m.PatchPath + "|" + m.EnginePath
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ManagedConflict{PatchPath: m.PatchPath, EnginePath: m.EnginePath, Desired: m.Value, New: nv})
		}
	}
	return out
}

func pathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}
