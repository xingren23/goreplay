package main

import (
	"bytes"
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
	plugins *InOutPlugins
}

// NewEmitter creates and initializes new Emitter object.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// Start initialize loop for sending data from inputs to outputs
func (e *Emitter) Start(plugins *InOutPlugins, middlewareCmd string) {
	if Settings.InputRAWConfig.CopyBufferSize < 1 {
		Settings.InputRAWConfig.CopyBufferSize = 5 << 20
	}

	// Optimisation to not call reflect on each emit (and yes I did not wanted to add new Service interface)
	e.plugins = plugins

	if middlewareCmd != "" {
		middleware := NewMiddleware(middlewareCmd)

		for _, in := range plugins.Inputs {
			middleware.ReadFrom(in)
		}

		e.plugins.Inputs = append(e.plugins.Inputs, middleware)
		e.plugins.All = append(e.plugins.All, middleware)
		e.Add(1)
		go func() {
			defer e.Done()
			if err := e.CopyMulty(middleware, plugins.Outputs...); err != nil {
				Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
			}
		}()
	} else {
		for _, in := range plugins.Inputs {
			e.Add(1)
			go func(in PluginReader) {
				defer e.Done()
				if err := e.CopyMulty(in, plugins.Outputs...); err != nil {
					Debug(2, fmt.Sprintf("[EMITTER] error during copy: %q", err))
				}
			}(in)
		}
	}
}

// Close closes all the goroutine and waits for it to finish.
func (e *Emitter) Close() {
	for _, p := range e.plugins.All {
		if cp, ok := p.(io.Closer); ok {
			cp.Close()
		}
	}
	if len(e.plugins.All) > 0 {
		// wait for everything to stop
		e.Wait()
	}
	e.plugins.All = nil // avoid Close to make changes again
}

// CopyMulty copies from 1 reader to multiple writers
func (e *Emitter) CopyMulty(src PluginReader, writers ...PluginWriter) error {
	wIndex := 0
	globalModifier := NewHTTPModifier(&Settings.ModifierConfig)
	filteredRequests := make(map[string]int64)
	filteredRequestsLastCleanTime := time.Now().UnixNano()
	filteredCount := 0

	// Optimisatio to not check service ID on each write
	var outServices [][]byte
	serviceModifiers := make(map[string]*HTTPModifier)
	for _, p := range writers {
		outServices = append(outServices, []byte(reflect.ValueOf(p).Elem().FieldByName("Service").String()))
	}

	for s, cfg := range Settings.Services {
		serviceModifiers[s] = NewHTTPModifier(&cfg.ModifierConfig)
	}

	for {
		msg, err := src.PluginRead()
		if err != nil {
			if err == ErrorStopped || err == io.EOF {
				return nil
			}
			return err
		}
		if msg != nil && len(msg.Data) > 0 {
			if len(msg.Data) > int(Settings.InputRAWConfig.CopyBufferSize) {
				msg.Data = msg.Data[:Settings.InputRAWConfig.CopyBufferSize]
			}
			meta := payloadMeta(msg.Meta)
			if len(meta) < 3 {
				Debug(2, fmt.Sprintf("[EMITTER] Found malformed record %q from %q", msg.Meta, src))
				continue
			}
			requestID := byteutils.SliceToString(meta[1])
			// start a subroutine only when necessary
			if Settings.Verbose >= 3 {
				Debug(3, "[EMITTER] input: ", byteutils.SliceToString(msg.Meta[:len(msg.Meta)-1]), " from: ", src)
			}
			if globalModifier != nil {
				Debug(3, "[EMITTER] modifier:", requestID, "from:", src)
				if isRequestPayload(msg.Meta) {
					msg.Data = globalModifier.Rewrite(msg.Data)
					// If modifier tells to skip request
					if len(msg.Data) == 0 {
						filteredRequests[requestID] = time.Now().UnixNano()
						filteredCount++
						continue
					}

					if len(meta) > 4 && len(meta[4]) > 0 {
						if m, found := serviceModifiers[byteutils.SliceToString(meta[4])]; found {
							msg.Data = m.Rewrite(msg.Data)
							// If modifier tells to skip request
							if len(msg.Data) == 0 {
								filteredRequests[requestID] = time.Now().UnixNano()
								filteredCount++
								continue
							}
						}
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

			if Settings.PrettifyHTTP {
				msg.Data = prettifyHTTP(msg.Data)
				if len(msg.Data) == 0 {
					continue
				}
			}

			if Settings.SplitOutput {
				writerService := outServices[wIndex]
				if len(meta) > 4 && len(meta[4]) > 0 && len(writerService) > 0 && !bytes.Equal(meta[4], writerService) {
					continue
				}

				if Settings.RecognizeTCPSessions {
					if !PRO {
						log.Fatal("Detailed TCP sessions work only with PRO license")
					}
					hasher := fnv.New32a()
					hasher.Write(meta[1])

					wIndex = int(hasher.Sum32()) % len(writers)

					if _, err := writers[wIndex].PluginWrite(msg); err != nil {
						return err
					}
				} else {
					// Simple round robin
					if _, err := writers[wIndex].PluginWrite(msg); err != nil {
						return err
					}

					wIndex = (wIndex + 1) % len(writers)
				}
			} else {
				for dIdx, dst := range writers {
					writerService := outServices[dIdx]
					if len(meta) > 4 && len(meta[4]) > 0 && len(writerService) > 0 && !bytes.Equal(meta[4], writerService) {
						continue
					}

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
