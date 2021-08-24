package main

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppSettings(t *testing.T) {
	a := AppSettings{}
	_, err := json.Marshal(&a)
	if err != nil {
		t.Error(err)
	}
}

func TestConfig(t *testing.T) {
	var yamlExample = []byte(`
verbose: 2
input-raw: 80
output-dummy: true
services:
  foo:
    input-raw: 8080
    output-http: http://example.com
    http-allow-header: "single:.*"
    http-set-header: 
      - "Foo: bar"
      - "Bar: foo"
`)

	Settings = *NewAppSettings()
	loadConfig(yamlExample)
	defer func() {
		Settings = *NewAppSettings()
	}()

	expectedConfig := AppSettings{
		ServiceSettings: *NewServiceSettings(),
		Services: map[string]ServiceSettings{
			"foo": {
				InputRAW:   MultiOption{"8080"},
				OutputHTTP: MultiOption{"http://example.com"},
				ModifierConfig: HTTPModifierConfig{
					HeaderFilters: HTTPHeaderFilters{{[]byte("single"), regexp.MustCompile(".*")}},
					Headers:       HTTPHeaders{{Name: "Foo", Value: "bar"}, {Name: "Bar", Value: "foo"}},
				},
			},
		},
	}
	expectedConfig.Verbose = 2
	expectedConfig.ServiceSettings.InputRAW = MultiOption{"80"}
	expectedConfig.ServiceSettings.OutputDummy = MultiOption{"1"}

	assert.Equal(t, expectedConfig, Settings, "config should match")
}
