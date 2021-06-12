package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mattn/go-tty"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const cmdName = "kj"

const (
	exitStatusOK = iota
	exitStatusErr
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Usage = func() {
		fmt.Printf(`%[1]s - create custom job from cronjob template

Usage:
	%[1]s namespace name
	%[1]s namespace/name

Options:
`, cmdName)
		flag.PrintDefaults()
	}
	flag.Parse()

	clientset, err := newK8sClient(*kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: failed to connect kubernetes (%v)\n", cmdName, err)
		return exitStatusErr
	}

	namespace, name, ok := getNamespaceAndName(flag.Args())
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: argments are invalid\n", cmdName)
		flag.Usage()
		return exitStatusErr
	}

	cj, err := clientset.BatchV1beta1().CronJobs(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		return exitStatusErr
	}

	job, err := newJob(cj)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		return exitStatusErr
	}

	if err = createJob(job); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		return exitStatusErr
	}

	return exitStatusOK
}

func newK8sClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func getNamespaceAndName(s []string) (namespace, name string, ok bool) {
	if len(s) == 0 || len(s) > 2 {
		return "", "", false
	}

	if len(s) == 2 {
		return s[0], s[1], true
	}

	s = strings.Split(s[0], "/")
	if len(s) != 2 {
		return "", "", false
	}
	return s[0], s[1], true
}

func newJob(cj *batchv1beta1.CronJob) (*batchv1.Job, error) {
	suffix, err := randStr(6)
	if err != nil {
		return nil, err
	}
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cj.Namespace,
			Name:      fmt.Sprintf("%s-%s", cj.Name, suffix),
		},
		Spec: cj.Spec.JobTemplate.Spec,
	}
	return job, nil
}

func randStr(n int) (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"

	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	var builder strings.Builder
	for _, v := range b {
		builder.WriteByte(letters[int(v)%len(letters)])
	}
	return builder.String(), nil
}

func createJob(job *batchv1.Job) error {
	f, err := os.CreateTemp("", "kj.*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	encoder := yaml.NewEncoder(f)
	err = encoder.Encode(job)
	if err != nil {
		return err
	}

	if err = f.Close(); err != nil {
		return err
	}

	tty, err := tty.Open()
	if err != nil {
		return err
	}
	defer tty.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editorWithArgs := strings.Fields(editor)
	editorWithArgs = append(editorWithArgs, f.Name())

	cmd := exec.Command(editorWithArgs[0], editorWithArgs[1:]...)
	cmd.Stdin = tty.Input()
	cmd.Stdout = tty.Output()
	cmd.Stderr = tty.Output()
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("kubectl", "apply", "-f", f.Name())
	cmd.Stdin = tty.Input()
	cmd.Stdout = tty.Output()
	cmd.Stderr = tty.Output()
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
