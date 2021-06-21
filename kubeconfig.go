package main

import (
	"os"

	"github.com/goccy/go-yaml"
)

type Kubeconfig struct {
	Contexts       []KubeContexts `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
}

type KubeContexts struct {
	Context KubeContext `yaml:"context"`
	Name    string      `yaml:"name"`
}

type KubeContext struct {
	Namespace string `yaml:"namespace"`
}

func loadKubeconfig(path string) (k Kubeconfig, err error) {
	f, err := os.Open(path)
	if err != nil {
		return k, err
	}
	defer f.Close()

	d := yaml.NewDecoder(f)
	err = d.Decode(&k)
	return k, err
}

func (k Kubeconfig) CurrentNamespace() string {
	kc, ok := k.currentContext()
	if !ok {
		return ""
	}

	return kc.Namespace
}

func (k Kubeconfig) currentContext() (KubeContext, bool) {
	for _, kc := range k.Contexts {
		if kc.Name == k.CurrentContext {
			return kc.Context, true
		}
	}
	return KubeContext{}, false
}
