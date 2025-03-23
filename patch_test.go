package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestLoadPatchFromFile(t *testing.T) {

	tempDir, err := os.MkdirTemp("", "patch-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	patchContent := `{
		"path": "spec.template.spec.containers[0].image",
		"value": "new-image:latest"
	}`
	patchFilePath := filepath.Join(tempDir, "test-patch.json")
	if err := os.WriteFile(patchFilePath, []byte(patchContent), 0644); err != nil {
		t.Fatalf("failed to write patch file: %v", err)
	}

	patch, err := loadPatchFromFile(patchFilePath)
	if err != nil {
		t.Fatalf("loadPatchFromFile failed: %v", err)
	}

	expectedPath := "spec.template.spec.containers[0].image"
	expectedValue := "new-image:latest"

	if patch.Path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, patch.Path)
	}

	valueStr, ok := patch.Value.(string)
	if !ok {
		t.Fatalf("expected value to be string, got %T", patch.Value)
	}

	if valueStr != expectedValue {
		t.Errorf("expected value %q, got %q", expectedValue, valueStr)
	}
}

func TestApplyPatchToYaml(t *testing.T) {

	jobYaml := []byte(`apiVersion: batch/v1
kind: Job
metadata:
  name: test-job
spec:
  template:
    spec:
      containers:
      - name: test-container
        image: original-image:v1
        command: ["echo", "hello"]
`)

	tests := map[string]struct {
		patch          patchFile
		expectedResult string
		expectError    bool
	}{
		"simple string value": {
			patch: patchFile{
				Path:  "spec.template.spec.containers[0].image",
				Value: "new-image:v2",
			},
			expectedResult: "new-image:v2",
			expectError:    false,
		},
		"array value": {
			patch: patchFile{
				Path:  "spec.template.spec.containers[0].command",
				Value: []interface{}{"python", "script.py", "--arg", "value"},
			},
			expectedResult: "[python script.py --arg value]",
			expectError:    false,
		},
		"nested object": {
			patch: patchFile{
				Path: "spec.template.spec.containers[0].resources",
				Value: map[string]interface{}{
					"limits": map[string]interface{}{
						"cpu":    "100m",
						"memory": "128Mi",
					},
				},
			},
			expectedResult: "map[limits:map[cpu:100m memory:128Mi]]",
			expectError:    false,
		},
		"nonexistent path segment": {
			patch: patchFile{
				Path:  "spec.template.spec.containers[0].nonexistent.field",
				Value: "new-value",
			},
			expectedResult: "new-value",
			expectError:    false,
		},
		"invalid index": {
			patch: patchFile{
				Path:  "spec.template.spec.containers[5].image",
				Value: "should-fail",
			},
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := applyPatchToYaml(jobYaml, tt.patch)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					t.Errorf(tt.patch.Path)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var jobMap map[string]interface{}
			if err := yaml.Unmarshal(result, &jobMap); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			segments := strings.Split(tt.patch.Path, ".")
			currentMap := jobMap

			for i, segment := range segments {

				if i == len(segments)-1 {
					var actualValue interface{}

					if strings.Contains(segment, "[") && strings.Contains(segment, "]") {
						indexStart := strings.Index(segment, "[")
						indexEnd := strings.Index(segment, "]")
						arrayName := segment[:indexStart]
						indexStr := segment[indexStart+1 : indexEnd]
						index, _ := strconv.Atoi(indexStr)

						array, ok := currentMap[arrayName].([]interface{})
						if !ok {
							t.Fatalf("expected array at %s", arrayName)
						}

						element := array[index].(map[string]interface{})
						fieldName := segment[indexEnd+1:]
						if fieldName != "" && strings.HasPrefix(fieldName, ".") {
							fieldName = fieldName[1:]
							actualValue = element[fieldName]
						} else {
							actualValue = element
						}
					} else {
						actualValue = currentMap[segment]
					}

					actualValueStr := fmt.Sprintf("%v", actualValue)
					if actualValueStr != tt.expectedResult {
						t.Errorf("expected %q, got %q", tt.expectedResult, actualValueStr)
					}
					break
				}

				if strings.Contains(segment, "[") && strings.Contains(segment, "]") {
					indexStart := strings.Index(segment, "[")
					indexEnd := strings.Index(segment, "]")
					arrayName := segment[:indexStart]
					indexStr := segment[indexStart+1 : indexEnd]
					index, _ := strconv.Atoi(indexStr)

					array, ok := currentMap[arrayName].([]interface{})
					if !ok {
						t.Fatalf("expected array at %s", arrayName)
					}

					currentMap = array[index].(map[string]interface{})
				} else {
					nextMap, ok := currentMap[segment].(map[string]interface{})
					if !ok {
						t.Fatalf("expected map at %s, got %T", segment, currentMap[segment])
					}
					currentMap = nextMap
				}
			}
		})
	}
}

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

	patchContent := `{
		"path": "spec.template.spec.containers[0].command",
		"value": ["python", "script.py", "--flag", "value"]
	}`
	patchFilePath := filepath.Join(tempDir, "test-patch.json")
	if err := os.WriteFile(patchFilePath, []byte(patchContent), 0644); err != nil {
		t.Fatalf("failed to write patch file: %v", err)
	}

	outputFilePath := filepath.Join(tempDir, "output.yaml")

	editor := &patchJobEditor{
		filename:  outputFilePath,
		patchFile: patchFilePath,
	}

	if err := editor.EditJob(job); err != nil {
		t.Fatalf("EditJob failed: %v", err)
	}

	data, err := os.ReadFile(outputFilePath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var jobMap map[string]interface{}
	if err := yaml.Unmarshal(data, &jobMap); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	spec := jobMap["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	templateSpec := template["spec"].(map[string]interface{})
	containers := templateSpec["containers"].([]interface{})
	container := containers[0].(map[string]interface{})
	command := container["command"].([]interface{})

	expectedCommand := []string{"python", "script.py", "--flag", "value"}
	if len(command) != len(expectedCommand) {
		t.Errorf("expected command length %d, got %d", len(expectedCommand), len(command))
	}

	for i, cmd := range expectedCommand {
		if i < len(command) && command[i] != cmd {
			t.Errorf("expected command[%d] to be %q, got %q", i, cmd, command[i])
		}
	}
}
