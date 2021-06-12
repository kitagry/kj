package main

import "testing"

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
