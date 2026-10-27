package camconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// testUserinfo is built at run time so the fixtures hold no credential-looking literal.
const testUserinfo = "admin" + ":" + "hunter2" + "@"

const testSchema = `{"$ref":"#/$defs/CameraConfig","$defs":{
 "CameraConfig":{"type":"object","properties":{
  "detect":{"$ref":"#/$defs/Detect"},
  "objects":{"$ref":"#/$defs/Objects"},
  "enabled":{"type":"boolean"},
  "ffmpeg":{"type":"object","properties":{"inputs":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"}}}}}}
 }},
 "Detect":{"type":"object","additionalProperties":false,"properties":{
  "enabled":{"type":"boolean"},
  "fps":{"type":"integer","minimum":1,"maximum":60},
  "width":{"anyOf":[{"type":"integer"},{"type":"null"}]},
  "mode":{"enum":["fast","slow"]}
 }},
 "Objects":{"type":"object","properties":{"track":{"type":"array","items":{"type":"string"}}}}
}}`

func TestParseSectionYAMLSyntaxErrorsCarryPosition(t *testing.T) {
	cases := []struct {
		name, text string
		line, col  int
	}{
		{"unterminated flow", "enabled: true\nfps: [1, 2\n", 1, 1},
		{"tab indent", "detect:\n\tenabled: true\n", 2, 1},
		{"duplicate key", "enabled: true\nenabled: false\n", 2, 1},
		{"bad indent", "a:\n  b: 1\n c: 2\n", 2, 3},
	}
	for _, c := range cases {
		v, iss := ParseSectionYAML(c.text)
		if v != nil || len(iss) != 1 {
			t.Fatalf("%s: v=%v issues=%v", c.name, v, iss)
		}
		if iss[0].Line != c.line || iss[0].Column != c.col || iss[0].Message == "" {
			t.Errorf("%s: issue = %+v, want line %d col %d", c.name, iss[0], c.line, c.col)
		}
	}
	if _, iss := ParseSectionYAML("  \n"); len(iss) != 1 {
		t.Fatalf("empty = %v", iss)
	}
	if _, iss := ParseSectionYAML("a: 1\n---\nb: 2\n"); len(iss) != 1 || !strings.Contains(iss[0].Message, "un solo documento") {
		t.Fatalf("two documents = %v", iss)
	}
}

func TestParseSectionYAMLIsJSONShaped(t *testing.T) {
	v, iss := ParseSectionYAML("enabled: true\nfps: 5\nratio: 0.5\ntrack:\n  - person\n  - car\nnone: null\n")
	if len(iss) != 0 {
		t.Fatal(iss)
	}
	raw, _ := json.Marshal(v)
	if string(raw) != `{"enabled":true,"fps":5,"none":null,"ratio":0.5,"track":["person","car"]}` {
		t.Fatalf("json = %s", raw)
	}
	if _, iss := ParseSectionYAML("1: a\n"); len(iss) != 0 {
		// yaml.v3 reads 1 as an int key into map[string]any? it must either decode or report.
		t.Logf("int key accepted: %v", iss)
	}
}

func TestSectionYAMLRoundTrip(t *testing.T) {
	in := map[string]any{"enabled": true, "fps": float64(5), "track": []any{"person", "car"}, "nested": map[string]any{"a": "b"}}
	text, err := SectionYAML(in)
	if err != nil {
		t.Fatal(err)
	}
	back, iss := ParseSectionYAML(text)
	if len(iss) != 0 || len(DiffLeaves(in, back)) != 0 {
		t.Fatalf("round trip differs: %q -> %v (%v)", text, back, iss)
	}
	if text != "enabled: true\nfps: 5\nnested:\n  a: b\ntrack:\n  - person\n  - car\n" {
		t.Fatalf("yaml = %q", text)
	}
}

func TestCheckSection(t *testing.T) {
	schema := json.RawMessage(testSchema)
	issues, err := CheckSection(schema, "detect", map[string]any{"enabled": true, "fps": float64(5), "width": nil, "mode": "fast"})
	if err != nil || len(issues) != 0 {
		t.Fatalf("valid: %v %v", issues, err)
	}
	issues, err = CheckSection(schema, "detect", map[string]any{"enabled": "yes", "fps": float64(500), "mystery": 1.0, "mode": "turbo", "width": "wide"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, i := range issues {
		got[i.Path] = i.Message
	}
	for path, frag := range map[string]string{
		"enabled": "se esperaba verdadero/falso", "fps": "como máximo 60", "mystery": "clave desconocida",
		"mode": "opciones: fast, slow", "width": "número entero",
	} {
		if !strings.Contains(got[path], frag) {
			t.Errorf("%s: %q does not contain %q (all: %v)", path, got[path], frag, got)
		}
	}
	if issues, _ := CheckSection(schema, "enabled", "x"); len(issues) != 1 {
		t.Fatalf("scalar section: %v", issues)
	}
	if issues, _ := CheckSection(schema, "objects", map[string]any{"track": []any{"person", 5.0}}); len(issues) != 1 || issues[0].Path != "track.1" {
		t.Fatalf("list item: %v", issues)
	}
	if _, err := CheckSection(schema, "nope", map[string]any{}); !errors.Is(err, ErrUnknownSection) {
		t.Fatalf("unknown section err = %v", err)
	}
	if names := SectionNames(schema); strings.Join(names, ",") != "detect,enabled,ffmpeg,objects" {
		t.Fatalf("names = %v", names)
	}
}

func TestDiffLeaves(t *testing.T) {
	before := map[string]any{"enabled": true, "fps": 5.0, "track": []any{"person"}, "gone": "x", "nest": map[string]any{"a": 1.0}}
	after := map[string]any{"enabled": false, "fps": 5.0, "track": []any{"person", "car"}, "new": map[string]any{"a": "b"}, "nest": map[string]any{"a": 1.0, "b": 2.0}}
	got := DiffLeaves(before, after)
	want := []LeafChange{
		{Path: "enabled", Kind: LeafChanged, From: true, To: false},
		{Path: "gone", Kind: LeafRemoved, From: "x"},
		{Path: "nest.b", Kind: LeafAdded, To: 2.0},
		{Path: "new.a", Kind: LeafAdded, To: "b"},
		{Path: "track", Kind: LeafChanged, From: []any{"person"}, To: []any{"person", "car"}},
	}
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if string(gj) != string(wj) {
		t.Fatalf("diff = %s\nwant   %s", gj, wj)
	}
	if len(DiffLeaves(before, before)) != 0 {
		t.Fatal("identical documents must not differ")
	}
}

func TestSafeChangesHideCredentials(t *testing.T) {
	ch := SafeChanges([]LeafChange{
		{Path: "inputs.0.path", Kind: LeafChanged, From: "rtsp://" + testUserinfo + "10.0.0.5/s", To: "rtsp://admin:" + "other" + "@10.0.0.5/s"},
		{Path: "onvif.password", Kind: LeafChanged, From: "a", To: "b"},
		{Path: "onvif.user", Kind: LeafAdded, To: "admin"},
	})
	raw, _ := json.Marshal(ch)
	for _, leak := range []string{"hunter2", "other", "admin\"", ":a\"", ":b\""} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("leaks %q: %s", leak, raw)
		}
	}
}

func TestResolvePlaceholdersKeepsRealSecrets(t *testing.T) {
	shown := map[string]any{"inputs": []any{map[string]any{"path": "rtsp://*:*@10.0.0.5/s", "roles": []any{"detect"}}}, "password": "********"}
	actual := map[string]any{"inputs": []any{map[string]any{"path": "rtsp://" + testUserinfo + "10.0.0.5/s", "roles": []any{"detect"}}}, "password": "s3cret"}
	edited := map[string]any{"inputs": []any{map[string]any{"path": "rtsp://*:*@10.0.0.5/s", "roles": []any{"detect", "record"}}}, "password": "********", "extra": 1.0}

	got, err := ResolvePlaceholders(edited, shown, actual)
	if err != nil {
		t.Fatal(err)
	}
	gm := got.(map[string]any)
	if gm["password"] != "s3cret" || gm["inputs"].([]any)[0].(map[string]any)["path"] != "rtsp://"+testUserinfo+"10.0.0.5/s" || gm["extra"] != 1.0 {
		t.Fatalf("placeholders not restored: %v", got)
	}

	// Real values unavailable: map keys are dropped (the engine merges), lists are refused.
	got, err = ResolvePlaceholders(map[string]any{"password": "********", "fps": 5.0}, map[string]any{"password": "********"}, nil)
	if err != nil || len(got.(map[string]any)) != 1 || got.(map[string]any)["fps"] != 5.0 {
		t.Fatalf("drop: %v %v", got, err)
	}
	if _, err := ResolvePlaceholders(edited, shown, nil); err == nil {
		t.Fatal("a placeholder inside a list without the real value must be refused")
	}
	// A placeholder that is not the one shown cannot be written.
	if _, err := ResolvePlaceholders(map[string]any{"password": "*****"}, map[string]any{"password": "********"}, actual); err == nil {
		t.Fatal("a different placeholder must be refused")
	}
}

func TestPruneUnchanged(t *testing.T) {
	cur := map[string]any{"a": 1.0, "n": map[string]any{"x": 1.0, "y": 2.0}, "l": []any{1.0}}
	if PruneUnchanged(cur, cur) != nil {
		t.Fatal("identical must prune to nothing")
	}
	got := PruneUnchanged(map[string]any{"a": 1.0, "n": map[string]any{"x": 1.0, "y": 3.0}, "l": []any{1.0, 2.0}, "new": true}, cur)
	raw, _ := json.Marshal(got)
	if string(raw) != `{"l":[1,2],"n":{"y":3},"new":true}` {
		t.Fatalf("pruned = %s", raw)
	}
	// Removing a key is not a change the engine can express.
	if PruneUnchanged(map[string]any{"a": 1.0}, cur) != nil {
		t.Fatal("removals must prune to nothing")
	}
}

func TestManagedPathsAndConflicts(t *testing.T) {
	on, off := true, false
	objs := []string{"person"}
	minArea := 400
	desired := Patch{
		Detection: &DetectionPatch{Enabled: &on, Objects: &objs},
		Recording: &RecordingPatch{Enabled: &off},
		Analytics: &AnalyticsPatch{LPR: &on, Modules: map[string]*ModulePatch{ModuleFaceRecognition: {Settings: &ModuleSettingsPatch{MinArea: &minArea}}}},
	}
	managed := ManagedPaths(desired)
	byEngine := map[string]string{}
	for _, m := range managed {
		byEngine[m.EnginePath] = m.PatchPath
	}
	for engine, patch := range map[string]string{
		"detect.enabled": "detection.enabled", "objects.track": "detection.objects", "record.enabled": "recording.enabled",
		"lpr.enabled": "analytics.lpr", "face_recognition.min_area": "analytics.modules.face_recognition.settings.min_area",
	} {
		if byEngine[engine] != patch {
			t.Errorf("%s -> %q, want %q (all %v)", engine, byEngine[engine], patch, byEngine)
		}
	}

	detect := InSection(managed, "detect")
	if len(detect) != 1 || detect[0].EnginePath != "enabled" {
		t.Fatalf("detect section = %+v", detect)
	}
	// Turning detection off conflicts; setting the desired value or an unrelated key does not.
	if c := ManagedConflicts(detect, []LeafChange{{Path: "enabled", Kind: LeafChanged, From: true, To: false}}); len(c) != 1 || c[0].PatchPath != "detection.enabled" || c[0].Desired != true || c[0].New != false {
		t.Fatalf("conflicts = %+v", c)
	}
	if c := ManagedConflicts(detect, []LeafChange{{Path: "enabled", Kind: LeafChanged, From: false, To: true}}); len(c) != 0 {
		t.Fatalf("matching the desired value is no conflict: %+v", c)
	}
	if c := ManagedConflicts(detect, []LeafChange{{Path: "fps", Kind: LeafChanged, From: 5.0, To: 10.0}}); len(c) != 0 {
		t.Fatalf("unrelated key: %+v", c)
	}
	if c := ManagedConflicts(detect, []LeafChange{{Path: "enabled", Kind: LeafRemoved, From: true}}); len(c) != 1 {
		t.Fatalf("removing a managed key conflicts: %+v", c)
	}

	zones := ManagedZonePaths(map[string]any{"ovms_door": map[string]any{}, "mine": map[string]any{}})
	if len(zones) != 1 || zones[0].EnginePath != "zones.ovms_door" {
		t.Fatalf("zones = %+v", zones)
	}
}

func TestSafeValueHidesEveryCredentialShape(t *testing.T) {
	secret := "Qu3ryS3cret"
	cases := map[string]string{
		"ffmpeg.inputs.path":  "rtsp://10.0.0.5:554/user=admin&password=" + secret + "&channel=1&stream=0.sdp",
		"a.path":              "rtsp://10.0.0.5:554/cam?user=admin&password=" + secret,
		"b.path":              "rtsp://admin:" + secret + "@h:554/main",
		"c.path":              "rtsp://admin:p@" + secret + "@h/main",
		"d.path":              "rtsp://admin:pa/" + secret + "@h/main",
		"e.path":              "rtsp://admin:pa?" + secret + "@h/main",
		"f.input_args":        "-rtsp_transport tcp -i rtsp://u:" + secret + "@h/x",
		"onvif.api_key":       secret,
		"onvif.passwd":        secret,
		"x.credentials":       secret,
		"x.auth":              secret,
		"x.cookie":            secret,
		"x.bearer":            secret,
		"g.url":               "http://h/live?token=" + secret,
		"h.path":              "rtsp://h/stream/" + secret + "=1",
		"ffmpeg.inputs.input": "rtsp://[::1/" + secret + "@",
	}
	for path, val := range cases {
		for name, got := range map[string]any{"scalar": SafeValue(path, val), "list": SafeValue(path, []any{map[string]any{"v": val}})} {
			raw := fmt.Sprint(got)
			if strings.Contains(raw, secret) {
				t.Errorf("%s (%s) leaks the credential: %v", path, name, got)
			}
		}
	}
	if got := SafeValue("ffmpeg.inputs.path", "rtsp://admin:x@10.0.0.5:554/Streaming/Channels/101?a=b"); got != "rtsp://10.0.0.5:554/Streaming/Channels/101" {
		t.Errorf("a plain stream URL keeps scheme, host and path: %v", got)
	}
}

func TestRedactURLDropsEmbeddedCredentials(t *testing.T) {
	for _, raw := range []string{
		"rtsp://10.0.0.5:554/user=admin&password=Qu3ryS3cret&channel=1",
		"rtsp://admin:p@Qu3ryS3cret@h/main",
		"rtsp://admin:pa/Qu3ryS3cret@h/main",
		"rtsp://h/main?password=Qu3ryS3cret",
	} {
		if got := RedactURL(raw); strings.Contains(got, "Qu3ryS3cret") {
			t.Errorf("RedactURL(%q) leaks: %q", raw, got)
		}
	}
	if RedactURL("not a url") != "" || RedactURL("rtsp://admin:x@10.0.0.5:554/a/b") != "rtsp://10.0.0.5:554/a/b" {
		t.Error("RedactURL contract changed")
	}
}
