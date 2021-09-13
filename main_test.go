package main

import (
	"testing"

	"k8s.io/apimachinery/pkg/version"
)

func TestGetNamespaceAndName(t *testing.T) {
	tests := map[string]struct {
		inputs          []string
		namespace, name string
		ok              bool
	}{
		"namespace and name is separated": {
			inputs:    []string{"namespace", "name"},
			namespace: "namespace",
			name:      "name",
			ok:        true,
		},
		"namespace and name is concatenated": {
			inputs:    []string{"namespace/name"},
			namespace: "namespace",
			name:      "name",
			ok:        true,
		},
		"only name is specified": {
			inputs:    []string{"name"},
			namespace: "",
			name:      "name",
			ok:        true,
		},
		"inputs are nil": {
			inputs: nil,
			ok:     false,
		},
		"separator is too many": {
			inputs: []string{"namespace/name/hello"},
			ok:     false,
		},
		"inputs are too many": {
			inputs: []string{"namespace", "name", "hello"},
			ok:     false,
		},
	}

	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			namespace, name, ok := getNamespaceAndName(tt.inputs)
			if namespace != tt.namespace {
				t.Errorf(`namespace expected "%s", but got "%s"`, tt.namespace, namespace)
			}
			if name != tt.name {
				t.Errorf(`name expected "%s", but got "%s"`, tt.name, name)
			}
			if ok != tt.ok {
				t.Errorf(`ok expected %v, but got %v`, tt.ok, ok)
			}
		})
	}
}

func TestIsCronJobGA(t *testing.T) {
	tests := map[string]struct {
		input  *version.Info
		expect bool
	}{
		"1.20": {
			input: &version.Info{
				Major: "1",
				Minor: "20",
			},
			expect: false,
		},
		"1.21": {
			input: &version.Info{
				Major: "1",
				Minor: "21",
			},
			expect: true,
		},
		"1.19+": {
			input: &version.Info{
				Major: "1",
				Minor: "19+",
			},
			expect: false,
		},
		"1.21+": {
			input: &version.Info{
				Major: "1",
				Minor: "21+",
			},
			expect: true,
		},
	}

	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			got := isCronJobGA(tt.input)
			if got != tt.expect {
				t.Errorf("isCronJobGA expect %t, got %t", tt.expect, got)
			}
		})
	}
}
