package httpapi_test

import (
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// jsonFields lists the JSON names of a struct's fields (embedded structs
// flattened) and whether each is always present / may be null.
type jsonField struct {
	omitempty bool
	nullable  bool
}

func jsonFields(t reflect.Type) map[string]jsonField {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]jsonField{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			for k, v := range jsonFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = jsonField{omitempty: strings.Contains(opts, "omitempty"), nullable: f.Type.Kind() == reflect.Pointer}
	}
	return out
}

// schemaAt resolves a JSON pointer (and $ref / allOf) into the merged
// properties and required list of an object schema.
func schemaAt(t *testing.T, ptr ...string) (props map[string]any, required []string) {
	t.Helper()
	s := get(spec.doc, ptr...)
	if s == nil {
		t.Fatalf("no schema at %v", ptr)
	}
	props = map[string]any{}
	var merge func(s any)
	merge = func(s any) {
		if ref, ok := get(s, "$ref").(string); ok {
			s = get(spec.doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
		}
		if p, ok := get(s, "properties").(map[string]any); ok {
			for k, v := range p {
				props[k] = v
			}
		}
		if r, ok := get(s, "required").([]any); ok {
			for _, x := range r {
				required = append(required, x.(string))
			}
		}
		if all, ok := get(s, "allOf").([]any); ok {
			for _, x := range all {
				merge(x)
			}
		}
	}
	merge(s)
	return props, required
}

func allowsNull(s any) bool {
	if ref, ok := get(s, "$ref").(string); ok {
		s = get(spec.doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
	}
	switch tp := get(s, "type").(type) {
	case []any:
		return slices.Contains(tp, any("null"))
	case string:
		return tp == "null"
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if arr, ok := get(s, k).([]any); ok {
			for _, x := range arr {
				if allowsNull(x) {
					return true
				}
			}
		}
	}
	return false
}

func schemaPtr(s string) []string { return strings.Split(s, "/") }

const (
	jsonResp = "content/application~1json/schema"
)

func pathSchema(path, method, rest string) []string {
	parts := []string{"paths", path, method}
	for _, p := range strings.Split(rest, "/") {
		parts = append(parts, strings.ReplaceAll(p, "~1", "/"))
	}
	return parts
}

// The documented schemas list exactly the fields the Go types send: no
// undocumented field, no documented field the API never sends. Fields
// always present are required, pointer fields are nullable.
func TestResponseSchemasMatchGoTypes(t *testing.T) {
	cases := []struct {
		ptr []string
		v   any
	}{
		{schemaPtr("components/schemas/Item"), app.Item{}},
		{schemaPtr("components/schemas/Collection"), app.Collection{}},
		{schemaPtr("components/schemas/ActionDef"), app.ActionDef{}},
		{schemaPtr("components/schemas/MediaRef"), app.MediaRef{}},
		{schemaPtr("components/schemas/Media"), app.Media{}},
		{schemaPtr("components/schemas/Event"), app.Event{}},
		{schemaPtr("components/schemas/EventPage"), app.EventPage{}},
		{schemaPtr("components/schemas/ItemPage"), app.ItemPage{}},
		{schemaPtr("components/schemas/Webhook"), app.Webhook{}},
		{schemaPtr("components/schemas/Delivery"), app.Delivery{}},
		{schemaPtr("components/schemas/Watcher"), app.Watcher{}},
		{schemaPtr("components/schemas/Plan"), app.Plan{}},
		{schemaPtr("components/schemas/Plan/properties/limits"), app.PlanLimits{}},
		{schemaPtr("components/schemas/Workspace"), app.Workspace{}},
		{schemaPtr("components/schemas/APIKey"), app.APIKey{}},
		{schemaPtr("components/schemas/Session"), app.Session{}},
		{schemaPtr("components/schemas/NotificationChannel"), app.NotificationChannel{}},
		{schemaPtr("components/schemas/NotificationDelivery"), app.NotificationDelivery{}},
		{schemaPtr("components/schemas/WorkspaceExport"), app.WorkspaceExport{}},
		{schemaPtr("components/schemas/PurgeResult"), app.PurgeResult{}},
		{schemaPtr("components/schemas/Billing"), app.Billing{}},
		{schemaPtr("components/schemas/ErrorBody"), app.Error{}},
		{schemaPtr("components/schemas/Collection/properties/cover"), app.Cover{}},
		{pathSchema("/v1/device-links", "post", "responses/201/"+jsonResp), app.DeviceLink{}},
		{pathSchema("/v1/workspace/usage", "get", "responses/200/"+jsonResp), app.Usage{}},
		{schemaPtr("components/schemas/Usage"), app.Usage{}}, // same shape, for POST /v1/workspace/plan
		{pathSchema("/v1/watchers/{id}/run", "post", "responses/200/"+jsonResp), app.WatcherRun{}},
		{pathSchema("/v1/items/bulk-actions", "post", "responses/200/"+jsonResp+"/properties/data/items"), app.BulkResult{}},
		{pathSchema("/v1/items/{id}/versions", "get", "responses/200/"+jsonResp+"/properties/data/items"), app.ItemVersion{}},
	}
	for _, c := range cases {
		name := strings.Join(c.ptr, "/")
		props, required := schemaAt(t, c.ptr...)
		fields := jsonFields(reflect.TypeOf(c.v))
		var missing, extra, notRequired, notNullable []string
		for k, f := range fields {
			p, ok := props[k]
			if !ok {
				missing = append(missing, k)
				continue
			}
			if !f.omitempty && !slices.Contains(required, k) {
				notRequired = append(notRequired, k)
			}
			if f.nullable && !f.omitempty && !allowsNull(p) {
				notNullable = append(notNullable, k)
			}
		}
		for k := range props {
			if _, ok := fields[k]; !ok {
				extra = append(extra, k)
			}
		}
		for _, l := range [][]string{missing, extra, notRequired, notNullable} {
			sort.Strings(l)
		}
		if len(missing) > 0 {
			t.Errorf("%s: fields sent by the API but not documented: %v", name, missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s: documented fields the API never sends: %v", name, extra)
		}
		if len(notRequired) > 0 {
			t.Errorf("%s: always-present fields not marked required: %v", name, notRequired)
		}
		if len(notNullable) > 0 {
			t.Errorf("%s: fields that can be null not documented as nullable: %v", name, notNullable)
		}
	}
}

// Request schemas list exactly the fields the handlers decode (the decoder
// rejects unknown fields, so an undocumented one is unusable and a missing
// one is a 400).
func TestRequestSchemasMatchGoTypes(t *testing.T) {
	body := func(path, method string) []string {
		return pathSchema(path, method, "requestBody/"+jsonResp)
	}
	edits := reflect.TypeOf(app.ActionInput{}).Field(1).Type // *itemFields
	cases := []struct {
		ptr []string
		t   reflect.Type
	}{
		{schemaPtr("components/schemas/CollectionInput"), reflect.TypeOf(app.CollectionInput{})},
		{schemaPtr("components/schemas/ItemFields"), edits},
		{schemaPtr("components/schemas/CreateItemInput"), reflect.TypeOf(app.CreateItemInput{})},
		{schemaPtr("components/schemas/UpdateItemInput"), reflect.TypeOf(app.UpdateItemInput{})},
		{schemaPtr("components/schemas/WebhookInput"), reflect.TypeOf(app.WebhookInput{})},
		{schemaPtr("components/schemas/WatcherInput"), reflect.TypeOf(app.WatcherInput{})},
		{schemaPtr("components/schemas/NotificationChannelInput"), reflect.TypeOf(app.ChannelInput{})},
		{schemaPtr("components/schemas/ActionDefInput"), reflect.TypeOf(app.ActionDef{})},
		{schemaPtr("components/schemas/NotificationChannelConfig"), reflect.TypeOf(app.ChannelConfig{})},
		{body("/v1/api-keys", "post"), reflect.TypeOf(app.CreateAPIKeyInput{})},
		{body("/v1/items/{id}/actions/{action}", "post"), reflect.TypeOf(app.ActionInput{})},
		{body("/v1/items/{id}/result", "post"), reflect.TypeOf(app.ResultInput{})},
		{body("/v1/items/bulk-actions", "post"), reflect.TypeOf(struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
			app.ActionInput
		}{})},
	}
	for _, c := range cases {
		name := strings.Join(c.ptr, "/")
		props, _ := schemaAt(t, c.ptr...)
		fields := jsonFields(c.t)
		var missing, extra []string
		for k := range fields {
			if _, ok := props[k]; !ok {
				missing = append(missing, k)
			}
		}
		for k := range props {
			if _, ok := fields[k]; !ok {
				extra = append(extra, k)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			t.Errorf("%s: fields the API accepts but not documented: %v", name, missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s: documented fields the API rejects: %v", name, extra)
		}
	}
}

// GET /v1/workspace/usage keeps its schema inline (the Python SDK's contract
// test reads it there); it must stay identical to components/schemas/Usage.
func TestUsageSchemaInSync(t *testing.T) {
	inline := get(spec.doc, pathSchema("/v1/workspace/usage", "get", "responses/200/"+jsonResp)...)
	comp := maps.Clone(get(spec.doc, "components", "schemas", "Usage").(map[string]any))
	delete(comp, "description")
	if !reflect.DeepEqual(inline, comp) {
		t.Fatal("GET /v1/workspace/usage and components/schemas/Usage differ")
	}
}
