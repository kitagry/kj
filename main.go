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

	"github.com/mattn/go-tty"
	"github.com/mattn/go-tty/ttyutil"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/yaml"
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
	filename := flag.String("f", "", "(optional) filename to save Job resource")
	flag.Usage = func() {
		fmt.Printf(`%[1]s - create custom job from cronjob template

Usage:
	%[1]s namespace name
	%[1]s namespace/name
	%[1]s name

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

	if namespace == "" {
		kc, err := loadKubeconfig(*kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		}
		namespace = kc.CurrentNamespace()
	}

	job, err := newJob(context.Background(), clientset, namespace, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		return exitStatusErr
	}

	if err = createJobWithFileName(filename, job); err != nil {
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
	if len(s) > 2 {
		return "", "", false
	}

	if len(s) == 1 {
		return "", s[0], true
	}

	return s[0], s[1], true
}

func newJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*batchv1.Job, error) {
	jobSpec, err := newJobTemplate(ctx, clientset, namespace, name)
	if err != nil {
		return nil, err
	}

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
			Namespace: namespace,
			Name:      fmt.Sprintf("%s-%s", name, suffix),
		},
		Spec: jobSpec,
	}
	return job, nil
}

func newJobTemplate(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (batchv1.JobSpec, error) {
	v, err := clientset.ServerVersion()
	if err != nil {
		return batchv1.JobSpec{}, fmt.Errorf("failed to get serverVersion: %w", err)
	}

	// When kubernetes version is 1.21 or higher, use batchv1.CronJob.
	// Otherwise, use batchv1beta1.CronJob
	if isCronJobGA(v) {
		cj, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return batchv1.JobSpec{}, err
		}
		return cj.Spec.JobTemplate.Spec, nil
	}

	cj, err := clientset.BatchV1beta1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return batchv1.JobSpec{}, err
	}
	return cj.Spec.JobTemplate.Spec, nil
}

func isCronJobGA(v *version.Info) bool {
	if (v.Major == "1" && v.Minor >= "21") || v.Major > "1" {
		return true
	}
	return false
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

func createJobWithFileName(filename *string, job *batchv1.Job) error {
	var f *os.File
	var err error
	if filename == nil || *filename == "" {
		f, err = os.CreateTemp("", "kj.*.yaml")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
	} else {
		f, err = os.Create(*filename)
		if err != nil {
			return err
		}
	}

	return createJob(f, job)
}

func createJob(f *os.File, job *batchv1.Job) error {
	data, err := yaml.Marshal(job)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
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

	fmt.Fprint(tty.Output(), "Can you apply it? [y/N]\n")
	answer, err := ttyutil.ReadLine(tty)
	if err != nil {
		return err
	}
	answer = strings.TrimSpace(answer)
	if answer != "Y" && answer != "y" {
		fmt.Println("canceled")
		return nil
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
