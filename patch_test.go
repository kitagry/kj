package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestPatchJobEditor(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "editor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    "test-container",
							Image:   "original-image:v1",
							Command: []string{"echo", "hello"},
						},
					},
				},
			},
		},
	}

	tests := map[string]struct {
		patchContent    string
		expectedImage   string
		expectedCommand []string
	}{
		"JSON patch format": {
			patchContent: `{
				"spec": {
					"template": {
						"spec": {
							"containers": [
								{
									"name": "test-container",
									"image": "new-image:v2",
									"command": ["python", "script.py", "--flag", "value"]
								}
							]
						}
					}
				}
			}`,
			expectedImage:   "new-image:v2",
			expectedCommand: []string{"python", "script.py", "--flag", "value"},
		},
		"YAML patch format": {
			patchContent: `
spec:
  template:
    spec:
      containers:
        - name: test-container
          image: yaml-image:latest
          command:
            - node
            - server.js
            - --port=8080
`,
			expectedImage:   "yaml-image:latest",
			expectedCommand: []string{"node", "server.js", "--port=8080"},
		},
		"minimal JSON patch": {
			patchContent:    `{"spec":{"template":{"spec":{"containers":[{"name":"test-container","image":"minimal-image"}]}}}}`,
			expectedImage:   "minimal-image",
			expectedCommand: []string{"echo", "hello"},
		},
		"minimal YAML patch": {
			patchContent: `
spec:
  template:
    spec:
      containers:
        - name: test-container
          image: minimal-yaml-image
`,
			expectedImage:   "minimal-yaml-image",
			expectedCommand: []string{"echo", "hello"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			patchFilePath := filepath.Join(tempDir, name+"-patch.yaml")
			if err := os.WriteFile(patchFilePath, []byte(tt.patchContent), 0644); err != nil {
				t.Fatalf("failed to write patch file: %v", err)
			}

			outputFilePath := filepath.Join(tempDir, name+"-output.yaml")

			editor := &patchJobEditor{
				filename:  outputFilePath,
				patchFile: patchFilePath,
			}

			jobCopy := job.DeepCopy()
			if err := editor.EditJob(jobCopy); err != nil {
				t.Fatalf("EditJob failed: %v", err)
			}

			data, err := os.ReadFile(outputFilePath)
			if err != nil {
				t.Fatalf("failed to read output file: %v", err)
			}

			jsonData, err := yaml.YAMLToJSON(data)
			if err != nil {
				t.Fatalf("failed to convert YAML to JSON: %v", err)
			}

			patchedJob := &batchv1.Job{}
			if err := yaml.Unmarshal(jsonData, patchedJob); err != nil {
				t.Fatalf("failed to unmarshal patched job: %v", err)
			}

			if len(patchedJob.Spec.Template.Spec.Containers) != 1 {
				t.Fatalf("expected 1 container, got %d", len(patchedJob.Spec.Template.Spec.Containers))
			}

			container := patchedJob.Spec.Template.Spec.Containers[0]
			if container.Image != tt.expectedImage {
				t.Errorf("expected image %q, got %q", tt.expectedImage, container.Image)
			}

			if diff := cmp.Diff(tt.expectedCommand, container.Command); diff != "" {
				t.Errorf("unexpected command diff: %s", diff)
			}
		})
	}
}
