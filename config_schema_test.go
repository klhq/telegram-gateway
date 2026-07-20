package main

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestPublicConfigSchema(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	schema, err := compiler.Compile("config.schema.json")
	if err != nil {
		t.Fatalf("compile config schema: %v", err)
	}

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:   "secret-free production config",
			config: `{"port":"8000","routes":{"strategy-a":"http://strategy-a:8080/callback"}}`,
		},
		{
			name:   "legacy config with inline secrets",
			config: `{"telegram_bot_token":"token","gateway_api_key":"key","webhook_secret":"secret"}`,
		},
		{
			name:    "unknown property",
			config:  `{"telegram_bot_token":"token","unknown":true}`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, err := jsonschema.UnmarshalJSON(strings.NewReader(test.config))
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			err = schema.Validate(instance)
			if test.wantErr && err == nil {
				t.Fatal("expected schema validation error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected schema validation error: %v", err)
			}
		})
	}
}
