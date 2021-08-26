// +build !race

package main

import (
	"sync"
	"testing"
)

func TestOutputLimiter(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewLimiter("test", NewTestOutput(func(*Message) {
		wg.Done()
	}), "10")
	wg.Add(10)

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 100; i++ {
		input.EmitGET()
	}

	wg.Wait()
	emitter.Close()
}

func TestInputLimiter(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewLimiter("test", NewTestInput(), "10")
	output := NewTestOutput(func(*Message) {
		wg.Done()
	})
	wg.Add(10)

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 100; i++ {
		input.(*Limiter).plugin.(*TestInput).EmitGET()
	}

	wg.Wait()
	emitter.Close()
}

// Should limit All requests
func TestPercentLimiter1(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewLimiter("test", NewTestOutput(func(*Message) {
		wg.Done()
	}), "0%")

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 100; i++ {
		input.EmitGET()
	}

	wg.Wait()
}

// Should not limit at All
func TestPercentLimiter2(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewLimiter("test", NewTestOutput(func(*Message) {
		wg.Done()
	}), "100%")
	wg.Add(100)

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 100; i++ {
		input.EmitGET()
	}

	wg.Wait()
}
