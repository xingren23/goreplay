package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/buger/goreplay/byteutils"
)

// Emitter represents an abject to manage plugins communication
type Emitter struct {
	sync.WaitGroup
	AppPlugins *AppPlugins
}

// NewEmitter creates and initializes new Emitter object.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// Start initialize loop for sending data from inputs to outputs
func (e *Emitter) Start(appPlugins *AppPlugins, middlewareCmd string) {
	e.AppPlugins = appPlugins

	if middlewareCmd != "" {
		// TODO : middle for All service, now middle only for global
		if e.AppPlugins.GlobalService != nil {
			middleware := NewMiddleware(middlewareCmd)
			for _, in := range e.AppPlugins.GlobalService.Inputs {
				middleware.ReadFrom(in)
			}

			e.AppPlugins.GlobalService.Inputs = append(e.AppPlugins.GlobalService.Inputs, middleware)
			e.Add(1)
			go func() {
				defer e.Done()
				if err := e.CopyMulty(middleware, e.AppPlugins.GlobalService.Outputs...); err != nil {
					Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
				}
			}()
		}
	} else {
		allServiceOutputs := make([]PluginWriter, 0)
		// start service
		if len(e.AppPlugins.Services) > 0 {
			for _, plugins := range e.AppPlugins.Services {
				serviceOutputs := plugins.Outputs
				allServiceOutputs = append(allServiceOutputs, plugins.Outputs...)
				if e.AppPlugins.GlobalService != nil {
					serviceOutputs = append(serviceOutputs, e.AppPlugins.GlobalService.Outputs...)
				}
				for _, in := range plugins.Inputs {
					e.Add(1)
					go func(in PluginReader, writers ...PluginWriter) {
						defer e.Done()
						if err := e.CopyMulty(in, writers...); err != nil {
							Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
						}
					}(in, serviceOutputs...)
				}
			}
		}

		// start global
		if e.AppPlugins.GlobalService != nil {
			allOutputs := append(allServiceOutputs, e.AppPlugins.GlobalService.Outputs...)
			for _, in := range e.AppPlugins.GlobalService.Inputs {
				e.Add(1)
				go func(in PluginReader, writers ...PluginWriter) {
					defer e.Done()
					if err := e.CopyMulty(in, writers...); err != nil {
						Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
					}
				}(in, allOutputs...)
			}
		}
	}
}

// Start service,
func (e *Emitter) AddService(service string, plugins *InOutPlugins) error {
	if e.AppPlugins == nil {
		return fmt.Errorf("emitter AppPlugins is nil, please start emitter first")
	}
	// TODO: incompatible with global
	if service == globalservice || (e.AppPlugins.GlobalService != nil && e.AppPlugins.GlobalService.Inputs != nil &&
		len(e.AppPlugins.GlobalService.Inputs) > 0) {
		return fmt.Errorf("emitter AddService incompatible with globalService, service %s", service)
	}

	// start service
	if _, ok := e.AppPlugins.Services[service]; !ok {
		e.AppPlugins.Services[service] = plugins
		serviceOutputs := plugins.Outputs
		if e.AppPlugins.GlobalService != nil {
			serviceOutputs = append(serviceOutputs, e.AppPlugins.GlobalService.Outputs...)
		}
		for _, in := range plugins.Inputs {
			e.Add(1)
			go func(in PluginReader, writers ...PluginWriter) {
				defer e.Done()
				if err := e.CopyMulty(in, writers...); err != nil {
					Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
				}
			}(in, serviceOutputs...)
		}
	} else {
		return fmt.Errorf("emitter service %s already exist", service)
	}
	return nil
}

// Cancel service
func (e *Emitter) CancelService(service string) error {
	if e.AppPlugins == nil {
		return fmt.Errorf("emitter AppPlugins is nil, please start emitter first")
	}
	// TODO: incompatible with global
	if service == globalservice || (e.AppPlugins.GlobalService != nil && e.AppPlugins.GlobalService.Inputs != nil &&
		len(e.AppPlugins.GlobalService.Inputs) > 0) {
		return fmt.Errorf("emitter AddService incompatible with globalService, service %s", service)
	}

	if plugins, ok := e.AppPlugins.Services[service]; ok {
		for _, in := range plugins.Inputs {
			if cp, ok := in.(io.Closer); ok {
				cp.Close()
			}
		}
		for _, out := range plugins.Outputs {
			if cp, ok := out.(io.Closer); ok {
				cp.Close()
			}
		}
	} else {
		return fmt.Errorf("service %s not exist", service)
	}
	return nil
}

// Get stats
func (e *Emitter) GetStats() map[string]string {
	stats := make(map[string]string)
	if e.AppPlugins.GlobalService != nil && e.AppPlugins.GlobalService.Inputs != nil {
		closed := e.AppPlugins.GlobalService.IsClosed()
		if closed {
			stats[globalservice] = "closed"
		} else {
			stats[globalservice] = "normal"
		}
	}
	for s, plugins := range e.AppPlugins.Services {
		closed := plugins.IsClosed()
		if closed {
			stats[s] = "closed"
		} else {
			stats[s] = "normal"
		}
	}
	return stats
}

// Close closes All the goroutine and waits for it to finish.
func (e *Emitter) Close() {
	for _, plugins := range e.AppPlugins.Services {
		plugins.Close()
	}
	e.AppPlugins.Services = make(map[string]*InOutPlugins)

	if e.AppPlugins.GlobalService != nil {
		e.AppPlugins.GlobalService.Close()
		e.AppPlugins.GlobalService = nil
	}
	e.Wait()
}

// CopyMulty copies from 1 reader to multiple writers
func (e *Emitter) CopyMulty(src PluginReader, writers ...PluginWriter) error {
	wIndex := 0
	modifier := NewHTTPModifier(&Settings.ModifierConfig)
	prettifyHttp := Settings.PrettifyHTTP
	splitOutput := Settings.SplitOutput
	recognizeTCPSessions := Settings.RecognizeTCPSessions
	filteredRequests := make(map[string]int64)
	filteredRequestsLastCleanTime := time.Now().UnixNano()
	filteredCount := 0

	service := reflect.ValueOf(src).Elem().FieldByName("Service").String()
	// global-input write to All output, other input write to the service's output
	var serviceWriters []PluginWriter
	if service == globalservice {
		serviceWriters = writers
		Debug(0, service, writers)
	} else {
		for _, p := range writers {
			srv := reflect.ValueOf(p).Elem().FieldByName("Service").String()
			if srv == globalservice {
				serviceWriters = append(serviceWriters, p)
			} else if srv == service {
				serviceWriters = append(serviceWriters, p)
			}
			Debug(0, service, srv, p)
		}
	}

	// replace with service's config
	for s, cfg := range Settings.Services {
		if s == service {
			modifier = NewHTTPModifier(&cfg.ModifierConfig)
			splitOutput = cfg.SplitOutput
			recognizeTCPSessions = cfg.RecognizeTCPSessions
		}
	}

	for {
		msg, err := src.PluginRead()
		if err != nil {
			if err == ErrorStopped || err == io.EOF {
				Debug(0, "Read Error Stopped")
				return nil
			}
			return err
		}
		if msg != nil && len(msg.Data) > 0 {
			if len(msg.Data) > int(Settings.CopyBufferSize) {
				msg.Data = msg.Data[:Settings.CopyBufferSize]
			}
			meta := payloadMeta(msg.Meta)
			if len(meta) < 3 {
				Debug(2, fmt.Sprintf("[EMITTER] Found malformed record %q from %q", msg.Meta, src))
				continue
			}
			requestID := byteutils.SliceToString(meta[1])
			Debug(3, "[EMITTER] input: ", byteutils.SliceToString(msg.Meta[:len(msg.Meta)-1]), " from: ", src)

			if modifier != nil {
				Debug(3, "[EMITTER] modifier:", requestID, "from:", src)
				if isRequestPayload(msg.Meta) {
					msg.Data = modifier.Rewrite(msg.Data)
					// If modifier tells to skip request
					if len(msg.Data) == 0 {
						filteredRequests[requestID] = time.Now().UnixNano()
						filteredCount++
						continue
					}
					Debug(3, "[EMITTER] Rewritten input:", requestID, "from:", src)

				} else {
					if _, ok := filteredRequests[requestID]; ok {
						delete(filteredRequests, requestID)
						filteredCount--
						continue
					}
				}
			}

			if prettifyHttp {
				msg.Data = prettifyHTTP(msg.Data)
				if len(msg.Data) == 0 {
					continue
				}
			}

			if splitOutput {
				if recognizeTCPSessions {
					if !PRO {
						log.Fatal("Detailed TCP sessions work only with PRO license")
					}
					hasher := fnv.New32a()
					hasher.Write(meta[1])

					wIndex = int(hasher.Sum32()) % len(serviceWriters)
					if _, err := serviceWriters[wIndex].PluginWrite(msg); err != nil {
						return err
					}
				} else {
					// Simple round robin
					if _, err := serviceWriters[wIndex].PluginWrite(msg); err != nil {
						return err
					}

					wIndex = (wIndex + 1) % len(serviceWriters)
				}
			} else {
				for _, dst := range serviceWriters {
					if _, err := dst.PluginWrite(msg); err != nil {
						return err
					}
				}
			}
		}

		// Run GC on each 1000 request
		if filteredCount > 0 && filteredCount%1000 == 0 {
			// Clean up filtered requests for which we didn't get a response to filter
			now := time.Now().UnixNano()
			if now-filteredRequestsLastCleanTime > int64(60*time.Second) {
				for k, v := range filteredRequests {
					if now-v > int64(60*time.Second) {
						delete(filteredRequests, k)
						filteredCount--
					}
				}
				filteredRequestsLastCleanTime = time.Now().UnixNano()
			}
		}
	}
}
