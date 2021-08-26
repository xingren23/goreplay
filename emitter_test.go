package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	PRO = true
	code := m.Run()
	os.Exit(code)
}

func TestEmitter(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		GlobalService: nil,
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()
	emitter.Close()
}

func TestEmitterAddService(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		GlobalService: new(InOutPlugins),
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}
	wgAdd := new(sync.WaitGroup)
	inputAdd := NewTestInput()
	inputAdd.Service = "add"
	outputAdd := NewTestOutput(func(*Message) {
		wgAdd.Done()
	})
	outputAdd.Service = "add"

	pluginsAdd := &InOutPlugins{
		Inputs:  []PluginReader{inputAdd},
		Outputs: []PluginWriter{outputAdd},
	}
	emitter.AddService("add", pluginsAdd)

	for i := 0; i < 1000; i++ {
		wgAdd.Add(1)
		inputAdd.EmitGET()
	}

	wg.Wait()
	wgAdd.Wait()
	emitter.Close()
}

func TestEmitterCancelService(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		GlobalService: nil,
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	err := emitter.CancelService("test")
	if err != nil {
		t.Errorf("cancel service failed, %v", err)
		return
	}
	if _, ok := emitter.AppPlugins.Services["test"]; !ok {
		t.Errorf("canceled service not existed")
	}

	stats := emitter.GetStats()
	if s, ok := stats["test"]; ok {
		if s != "closed" {
			t.Errorf("cancaled service stat failed")
			return
		}
	}
	emitter.Close()
}

func TestEmitterFiltered(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	input.skipHeader = true

	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	methods := HTTPMethods{[]byte("GET")}
	Settings.ModifierConfig = HTTPModifierConfig{Methods: methods}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, "")

	wg.Add(2)

	id := uuid()
	reqh := payloadHeader(RequestPayload, id, time.Now().UnixNano(), -1, "")
	reqb := append(reqh, []byte("POST / HTTP/1.1\r\nHost: www.w3.org\r\nUser-Agent: Go 1.1 package http\r\nAccept-Encoding: gzip\r\n\r\n")...)

	resh := payloadHeader(ResponsePayload, id, time.Now().UnixNano()+1, 1, "")
	respb := append(resh, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")...)

	input.EmitBytes(reqb)
	input.EmitBytes(respb)

	id = uuid()
	reqh = payloadHeader(RequestPayload, id, time.Now().UnixNano(), -1, "")
	reqb = append(reqh, []byte("GET / HTTP/1.1\r\nHost: www.w3.org\r\nUser-Agent: Go 1.1 package http\r\nAccept-Encoding: gzip\r\n\r\n")...)

	resh = payloadHeader(ResponsePayload, id, time.Now().UnixNano()+1, 1, "")
	respb = append(resh, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")...)

	input.EmitBytes(reqb)
	input.EmitBytes(respb)

	wg.Wait()
	emitter.Close()

	Settings.ModifierConfig = HTTPModifierConfig{}
}

func TestGlobalMultipleServices(t *testing.T) {
	globalWg := new(sync.WaitGroup)

	globalInput := NewTestInput()
	globalInput.Service = globalservice
	globalOutput := NewTestOutput(func(*Message) {
		globalWg.Done()
	})
	globalOutput.Service = globalservice

	service1Input := NewTestInput()
	service1Input.Service = "foo"

	wg1 := new(sync.WaitGroup)
	service1Output := NewTestOutput(func(*Message) {
		wg1.Done()
	})
	service1Output.Service = "foo"

	service2Input := NewTestInput()
	service2Input.Service = "bar"

	wg2 := new(sync.WaitGroup)
	service2Output := NewTestOutput(func(*Message) {
		wg2.Done()
	})
	service2Output.Service = "bar"

	appPlugins := &AppPlugins{
		GlobalService: &InOutPlugins{
			Inputs:  []PluginReader{globalInput},
			Outputs: []PluginWriter{globalOutput},
		},
		Services: map[string]*InOutPlugins{
			"foo": &InOutPlugins{
				Inputs:  []PluginReader{service1Input},
				Outputs: []PluginWriter{service1Output},
			},
			"bar": &InOutPlugins{
				Inputs:  []PluginReader{service2Input},
				Outputs: []PluginWriter{service2Output},
			},
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1; i++ {
		globalWg.Add(1)
		wg1.Add(1)
		wg2.Add(1)
		globalInput.EmitGET()

		globalWg.Add(1)
		wg1.Add(1)
		service1Input.EmitGET()

		globalWg.Add(1)
		wg2.Add(1)
		service2Input.EmitGET()
	}

	globalWg.Wait()
	wg1.Wait()
	wg2.Wait()

	emitter.Close()
}

func TestEmitterSplitRoundRobin(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()

	var counter1, counter2 int32

	output1 := NewTestOutput(func(*Message) {
		atomic.AddInt32(&counter1, 1)
		wg.Done()
	})

	output2 := NewTestOutput(func(*Message) {
		atomic.AddInt32(&counter2, 1)
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output1, output2},
	}

	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	Settings.SplitOutput = true

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()

	emitter.Close()

	if counter1 == 0 || counter2 == 0 || counter1 != counter2 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.SplitOutput = false
}

func TestEmitterRoundRobin(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()

	var counter1, counter2 int32

	output1 := NewTestOutput(func(*Message) {
		counter1++
		wg.Done()
	})

	output2 := NewTestOutput(func(*Message) {
		counter2++
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output1, output2},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	Settings.SplitOutput = true

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()
	emitter.Close()

	if counter1 == 0 || counter2 == 0 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.SplitOutput = false
}

func TestEmitterSplitSession(t *testing.T) {
	wg := new(sync.WaitGroup)
	wg.Add(200)

	input := NewTestInput()
	input.skipHeader = true

	var counter1, counter2 int32

	output1 := NewTestOutput(func(msg *Message) {
		if payloadID(msg.Meta)[0] == 'a' {
			counter1++
		}
		wg.Done()
	})

	output2 := NewTestOutput(func(msg *Message) {
		if payloadID(msg.Meta)[0] == 'b' {
			counter2++
		}
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{output1, output2},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	Settings.SplitOutput = true
	Settings.RecognizeTCPSessions = true

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 200; i++ {
		// Keep session but randomize
		id := make([]byte, 20)
		if i&1 == 0 { // for recognizeTCPSessions one should be odd and other will be even number
			id[0] = 'a'
		} else {
			id[0] = 'b'
		}
		input.EmitBytes([]byte(fmt.Sprintf("1 %s 1 1\nGET / HTTP/1.1\r\n\r\n", id[:20])))
	}

	wg.Wait()

	if counter1 != counter2 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.SplitOutput = false
	Settings.RecognizeTCPSessions = false
	emitter.Close()
}

func BenchmarkEmitter(b *testing.B) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()

	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()
	emitter.Close()
}
