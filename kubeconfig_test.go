package main

import (
	"reflect"
	"testing"
)

const (
	kubeconfigFilePath = "testdata/kubeconfig"
)

func TestLoadKubeConfig(t *testing.T) {
	k, err := loadKubeconfig(kubeconfigFilePath)
	if err != nil {
		t.Fatalf("failed to load kubeconfig: %+v", err)
	}

	expect := Kubeconfig{
		Contexts: []KubeContexts{
			{
				Context: KubeContext{
					Namespace: "",
				},
				Name: "a",
			},
			{
				Context: KubeContext{
					Namespace: "nsB",
				},
				Name: "b",
			},
		},
		CurrentContext: "b",
	}

	if !reflect.DeepEqual(k, expect) {
		t.Errorf("kubeconfig expected %+v, got %+v", expect, k)
	}
}

func TestKubeconfig_CurrentNamespace(t *testing.T) {
	tests := map[string]struct {
		currentContext string
		expect         string
	}{
		"Context has namespace": {
			currentContext: "b",
			expect:         "nsB",
		},
		"Context has no namespace": {
			currentContext: "a",
			expect:         "default",
		},
		"Context doesn't exist": {
			currentContext: "not exist context",
			expect:         "default",
		},
	}

	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			k, err := loadKubeconfig(kubeconfigFilePath)
			if err != nil {
				t.Fatalf("failed to load kubeconfig: %+v", err)
			}

			k.CurrentContext = tt.currentContext
			ns := k.CurrentNamespace()
			if ns != tt.expect {
				t.Errorf(`CurrentNamespace expected "%s", got "%s"`, tt.expect, ns)
			}
		})
	}
}
