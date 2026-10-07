// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package bus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// setCall is one SetMust call.
type setCall struct {
	// value is the new value.
	value any

	// property is the property name.
	property string
}

// countingSetter records every SetMust call.
type countingSetter struct {
	calls []setCall
}

// SetMust records the property and value.
func (s *countingSetter) SetMust(_, property string, value any) {
	s.calls = append(s.calls, setCall{value: value, property: property})
}

// FuzzPublish checks SetMust runs exactly when a property's value changes.
//
// Every string, including empty and invalid UTF-8, must be compared against
// the last published value of the same property and nothing else. Snapshot
// and settings publishes are interleaved, so a value matching the other
// property must still be emitted. The seeds cover empty values, repeats,
// values shared between the properties, and invalid UTF-8.
func FuzzPublish(f *testing.F) {
	f.Add("", "", "", "", "")
	f.Add("", "", `{"v":1}`, `{"v":1}`, `{"v":1}`)
	f.Add(`{"v":1}`, `{"v":2}`, `{"v":2}`, `{"v":1}`, `{"v":1}`)
	f.Add("a", "b", "a", "b", "c")
	f.Add("\xff", "\x00", "\xff", "\x00", "\xff")

	f.Fuzz(func(t *testing.T, snapshot, settings, first, second, third string) {
		setter := &countingSetter{calls: nil}
		service := NewService(setter, Exports{
			OnRefresh:     nil,
			OnSetSettings: nil,
			Snapshot:      snapshot,
			Settings:      settings,
			Version:       "",
		}, nil)

		steps := []struct {
			publish  func(string)
			property string
			value    string
		}{
			{publish: service.Publish, property: propertySnapshot, value: first},
			{publish: service.PublishSettings, property: propertySettings, value: first},
			{publish: service.Publish, property: propertySnapshot, value: second},
			{publish: service.PublishSettings, property: propertySettings, value: second},
			{publish: service.PublishSettings, property: propertySettings, value: third},
			{publish: service.Publish, property: propertySnapshot, value: second},
			{publish: service.Publish, property: propertySnapshot, value: third},
			{publish: service.PublishSettings, property: propertySettings, value: third},
		}

		want := []setCall{}
		last := map[string]string{propertySnapshot: snapshot, propertySettings: settings}

		for _, step := range steps {
			step.publish(step.value)

			if step.value != last[step.property] {
				want = append(want, setCall{value: step.value, property: step.property})
				last[step.property] = step.value
			}
		}

		assert.Equal(t, want, append([]setCall{}, setter.calls...))
		assert.Equal(t, last, service.last)
	})
}
