package main

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPInput(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewHTTPInput("127.0.0.1:0")
	input.Service = "test"
	time.Sleep(time.Millisecond)
	output := NewTestOutput(func(*Message) {
		wg.Done()
	})
	output.Service = "test"

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

	address := strings.Replace(input.address, "[::]", "127.0.0.1", -1)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		http.Get("http://" + address + "/")
	}

	wg.Wait()
	emitter.Close()
}

func TestInputHTTPLargePayload(t *testing.T) {
	wg := new(sync.WaitGroup)
	const n = 10 << 20 // 10MB
	var large [n]byte
	large[n-1] = '0'

	input := NewHTTPInput("127.0.0.1:0")
	input.Service = "test"
	output := NewTestOutput(func(msg *Message) {
		_len := len(msg.Data)
		if _len >= n { // considering http body CRLF
			t.Errorf("expected body to be >= %d", n)
		}
		wg.Done()
	})
	output.Service = "test"
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
	defer emitter.Close()
	go emitter.Start(appPlugins, Settings.Middleware)

	address := strings.Replace(input.address, "[::]", "127.0.0.1", -1)
	var req *http.Request
	var err error
	req, err = http.NewRequest("POST", "http://"+address, bytes.NewBuffer(large[:]))
	if err != nil {
		t.Error(err)
		return
	}
	wg.Add(1)
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
		return
	}
	wg.Wait()
}
