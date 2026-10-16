package app

import (
	"reflect"
	"slices"
)

// A state that is already set cannot be set again: archiving an archived
// collection, pausing a paused watcher, enabling an enabled rule and so on
// answer 409 instead of writing the row and emitting a second event. A PATCH
// that also changes other fields is not refused; the unchanged state field is
// simply a no-op there.

// onlyChange reports whether field (the Go field name of a PATCH input
// struct) is the only change requested: every other field is its zero value
// (nil pointer, nil map, false, ""). ignore lists fields that never count as a
// change (e.g. expected_version).
func onlyChange(in any, field string, ignore ...string) bool {
	v := reflect.ValueOf(in)
	t := v.Type()
	for i := range t.NumField() {
		name := t.Field(i).Name
		if name == field || slices.Contains(ignore, name) {
			continue
		}
		if !v.Field(i).IsZero() {
			return false
		}
	}
	return true
}

// alreadyErr is the 409 for re-applying the current state, e.g.
// "collection is already archived".
func alreadyErr(what, state string) *Error {
	return conflict(what + " is already " + state)
}
