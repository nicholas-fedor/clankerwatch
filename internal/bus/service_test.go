// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package bus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/nicholas-fedor/clankerwatch/internal/bus/mocks"
)

// publishers lists each publishing method with the property it sets.
//
// Publish and PublishSettings share their dedupe and recovery, so every
// behavior is checked for both.
var publishers = []struct {
	publish  func(service *Service, value string)
	name     string
	property string
}{
	{name: "Publish", property: propertySnapshot, publish: (*Service).Publish},
	{name: "PublishSettings", property: propertySettings, publish: (*Service).PublishSettings},
}

// TestNewService checks the service starts from the values props holds.
//
// The dedupe compares against these values, so a wrong start would either
// emit a duplicate signal or swallow the first real change.
func TestNewService(t *testing.T) {
	t.Parallel()

	props := mocks.NewMockPropertySetter(t)
	log := mocks.NewMockLogger(t)

	service := NewService(props, exports(`{"v":1}`, `{"v":2}`), log)

	assert.Same(t, props, service.props)
	assert.Same(t, log, service.log)
	assert.Equal(t, map[string]string{propertySnapshot: `{"v":1}`, propertySettings: `{"v":2}`}, service.last)
}

// TestPublishSetsANewValue checks a changed value reaches its property and
// leaves the other property's last value alone.
func TestPublishSetsANewValue(t *testing.T) {
	t.Parallel()

	for _, tt := range publishers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props := mocks.NewMockPropertySetter(t)
			props.EXPECT().SetMust(Interface, tt.property, "second").Return().Once()

			service := NewService(props, exports("first", "first"), mocks.NewMockLogger(t))
			tt.publish(service, "second")

			want := map[string]string{propertySnapshot: "first", propertySettings: "first"}
			want[tt.property] = "second"

			assert.Equal(t, want, service.last)
		})
	}
}

// TestPublishSkipsAnUnchangedValue checks an identical value emits nothing.
//
// The engine republishes on every tick and the daemon republishes the
// settings after every engine restart. Each emit wakes every widget on the
// panel, so a repeat must not reach the bus.
func TestPublishSkipsAnUnchangedValue(t *testing.T) {
	t.Parallel()

	for _, tt := range publishers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props := mocks.NewMockPropertySetter(t)

			service := NewService(props, exports("same", "same"), mocks.NewMockLogger(t))
			tt.publish(service, "same")

			props.AssertNotCalled(t, "SetMust", mock.Anything, mock.Anything, mock.Anything)
			assert.Equal(t, "same", service.last[tt.property])
		})
	}
}

// TestPublishDedupesPerProperty checks each property compares against its own
// last value.
//
// A snapshot equal to the current settings, or the reverse, is still a
// change for its own property and must be emitted.
func TestPublishDedupesPerProperty(t *testing.T) {
	t.Parallel()

	props := mocks.NewMockPropertySetter(t)
	props.EXPECT().SetMust(Interface, propertySnapshot, "shared").Return().Once()
	props.EXPECT().SetMust(Interface, propertySettings, "shared").Return().Once()

	service := NewService(props, exports("snapshot", "settings"), mocks.NewMockLogger(t))

	service.Publish("shared")
	service.PublishSettings("shared")
	service.Publish("shared")
	service.PublishSettings("shared")

	assert.Equal(t, map[string]string{propertySnapshot: "shared", propertySettings: "shared"}, service.last)
}

// TestPublishRecoversFromAFailedEmit checks a panicking emit is logged with
// the property name.
//
// prop.Properties.SetMust panics when the bus rejects the signal. The daemon
// must survive it and offer the same value again on the next publish.
func TestPublishRecoversFromAFailedEmit(t *testing.T) {
	t.Parallel()

	for _, tt := range publishers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props := mocks.NewMockPropertySetter(t)
			props.EXPECT().SetMust(Interface, tt.property, "next").
				Run(func(string, string, any) { panic("emit failed") }).Twice()

			log := mocks.NewMockLogger(t)
			log.EXPECT().Warn(mock.Anything, "Publishing a property failed",
				[]any{logKeyProperty, tt.property, logKeyErr, "emit failed"}).
				Return().Twice()

			service := NewService(props, exports("previous", "previous"), log)

			assert.NotPanics(t, func() { tt.publish(service, "next") })
			assert.Equal(t, "previous", service.last[tt.property], "a failed emit must not count as published")

			assert.NotPanics(t, func() { tt.publish(service, "next") })
		})
	}
}

// TestPublishAfterAFailedEmitSetsTheOtherProperty checks a failure on one
// property does not block the other.
func TestPublishAfterAFailedEmitSetsTheOtherProperty(t *testing.T) {
	t.Parallel()

	props := mocks.NewMockPropertySetter(t)
	props.EXPECT().SetMust(Interface, propertySnapshot, "broken").
		Run(func(string, string, any) { panic("emit failed") }).Once()
	props.EXPECT().SetMust(Interface, propertySettings, "fine").Return().Once()

	log := mocks.NewMockLogger(t)
	log.EXPECT().Warn(mock.Anything, "Publishing a property failed",
		[]any{logKeyProperty, propertySnapshot, logKeyErr, "emit failed"}).
		Return().Once()

	service := NewService(props, exports("", ""), log)

	assert.NotPanics(t, func() { service.Publish("broken") })
	service.PublishSettings("fine")

	assert.Equal(t, map[string]string{propertySnapshot: "", propertySettings: "fine"}, service.last)
}

// TestMethodIntrospection checks SetSettings declares one string in and one
// string out, which the plasmoid's D-Bus call relies on.
func TestMethodIntrospection(t *testing.T) {
	t.Parallel()

	methods := methodIntrospection()

	assert.Len(t, methods, 2)
	assert.Equal(t, methodRefresh, methods[0].Name)
	assert.Empty(t, methods[0].Args)
	assert.Equal(t, methodSetSettings, methods[1].Name)

	if assert.Len(t, methods[1].Args, 2) {
		assert.Equal(t, "s", methods[1].Args[0].Type)
		assert.Equal(t, "in", methods[1].Args[0].Direction)
		assert.Equal(t, "s", methods[1].Args[1].Type)
		assert.Equal(t, "out", methods[1].Args[1].Direction)
	}
}

// exports returns exports holding the initial values and no-op handlers.
func exports(snapshot, settings string) Exports {
	return Exports{
		OnRefresh:     func() {},
		OnSetSettings: func(string) string { return "" },
		Snapshot:      snapshot,
		Settings:      settings,
		Version:       "1.2.3",
	}
}
