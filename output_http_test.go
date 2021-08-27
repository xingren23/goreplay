package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	_ "net/http/httputil"
	"reflect"
	"sync"
	"testing"
)

func TestHTTPOutput(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("User-Agent") != "Gor" {
			t.Error("Wrong header")
		}

		if req.Method == "OPTIONS" {
			t.Error("Wrong method")
		}

		if req.Method == "POST" {
			defer req.Body.Close()
			body, _ := ioutil.ReadAll(req.Body)

			if string(body) != "a=1&b=2" {
				t.Error("Wrong POST body:", string(body))
			}
		}

		wg.Done()
	}))
	defer server.Close()

	headers := HTTPHeaders{httpHeader{"User-Agent", "Gor"}}
	methods := HTTPMethods{[]byte("GET"), []byte("PUT"), []byte("POST")}
	Settings.HTTPModifierConfig = HTTPModifierConfig{Headers: headers, Methods: methods}

	httpOutput := NewHTTPOutput(server.URL, &HTTPOutputConfig{TrackResponses: false})
	if reflect.ValueOf(httpOutput).IsNil() {
		t.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(httpOutput).Elem().FieldByName("Service").SetString("test")

	output := NewTestOutput(func(*Message) {
		wg.Done()
	})

	plugins := &InOutPlugins{
		Inputs:  []PluginReader{input},
		Outputs: []PluginWriter{httpOutput, output},
	}
	appPlugins := &AppPlugins{
		Services: map[string]*InOutPlugins{
			"test": plugins,
		},
	}

	emitter := NewEmitter()
	go emitter.Start(appPlugins, Settings.Middleware)

	for i := 0; i < 10; i++ {
		// 2 http-output, 2 - test output request
		wg.Add(4) // OPTIONS should be ignored
		input.EmitPOST()
		input.EmitOPTIONS()
		input.EmitGET()
	}

	wg.Wait()
	emitter.Close()

	Settings.HTTPModifierConfig = HTTPModifierConfig{}
}

func TestHTTPOutputKeepOriginalHost(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Host != "custom-host.com" {
			t.Error("Wrong header", req.Host)
		}

		wg.Done()
	}))
	defer server.Close()

	headers := HTTPHeaders{httpHeader{"Host", "custom-host.com"}}
	Settings.HTTPModifierConfig = HTTPModifierConfig{Headers: headers}

	output := NewHTTPOutput(server.URL, &HTTPOutputConfig{OriginalHost: true, SkipVerify: true})
	if reflect.ValueOf(output).IsNil() {
		t.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(output).Elem().FieldByName("Service").SetString("test")

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

	wg.Add(1)
	input.EmitGET()

	wg.Wait()
	emitter.Close()
	Settings.HTTPModifierConfig = HTTPModifierConfig{}
}

func TestHTTPOutputSSL(t *testing.T) {
	wg := new(sync.WaitGroup)

	// Origing and Replay server initialization
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wg.Done()
	}))

	input := NewTestInput()
	output := NewHTTPOutput(server.URL, &HTTPOutputConfig{SkipVerify: true})
	if reflect.ValueOf(output).IsNil() {
		t.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(output).Elem().FieldByName("Service").SetString("test")

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

	wg.Add(2)

	input.EmitPOST()
	input.EmitGET()

	wg.Wait()
	emitter.Close()
}

func TestHTTPOutputSessions(t *testing.T) {
	wg := new(sync.WaitGroup)

	input := NewTestInput()
	input.skipHeader = true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		wg.Done()
	}))
	defer server.Close()

	PRO = true
	Settings.RecognizeTCPSessions = true
	Settings.SplitOutput = true
	output := NewHTTPOutput(server.URL, &HTTPOutputConfig{})
	if reflect.ValueOf(output).IsNil() {
		t.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(output).Elem().FieldByName("Service").SetString("test")

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

	uuid1 := []byte("1234567890123456789a0000")
	uuid2 := []byte("1234567890123456789d0000")

	for i := 0; i < 10; i++ {
		wg.Add(1) // OPTIONS should be ignored
		copy(uuid1[20:], randByte(4))
		input.EmitBytes([]byte("1 " + string(uuid1) + " 1\n" + "GET / HTTP/1.1\r\n\r\n"))
	}

	for i := 0; i < 10; i++ {
		wg.Add(1) // OPTIONS should be ignored
		copy(uuid2[20:], randByte(4))
		input.EmitBytes([]byte("1 " + string(uuid2) + " 1\n" + "GET / HTTP/1.1\r\n\r\n"))
	}

	wg.Wait()

	emitter.Close()

	Settings.RecognizeTCPSessions = false
	Settings.SplitOutput = false
}

func BenchmarkHTTPOutput(b *testing.B) {
	wg := new(sync.WaitGroup)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wg.Done()
	}))
	defer server.Close()

	input := NewTestInput()
	output := NewHTTPOutput(server.URL, &HTTPOutputConfig{WorkersMax: 1})
	if reflect.ValueOf(output).IsNil() {
		b.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(output).Elem().FieldByName("Service").SetString("test")
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

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		input.EmitPOST()
	}

	wg.Wait()
	emitter.Close()
}

func BenchmarkHTTPOutputTLS(b *testing.B) {
	wg := new(sync.WaitGroup)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wg.Done()
	}))
	defer server.Close()

	input := NewTestInput()
	output := NewHTTPOutput(server.URL, &HTTPOutputConfig{SkipVerify: true, WorkersMax: 1})
	if reflect.ValueOf(output).IsNil() {
		b.Errorf("Construct service %s plugin failed", server.URL)
		return
	}
	reflect.ValueOf(output).Elem().FieldByName("Service").SetString("test")

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

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		input.EmitPOST()
	}

	wg.Wait()
	emitter.Close()
}
