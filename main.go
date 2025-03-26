package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/mattn/go-tty"
	"github.com/mattn/go-tty/ttyutil"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
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
	var (
		kubeconfig *string
		filename   *string
		patchFile  *string
	)
	// default kubeconfig path is loaded in the following priority:
	// 1. load environment variable KUBECONFIG exists
	// 2. load $HOME/.kube/config
	var defaultKubeConfig string
	if env := os.Getenv("KUBECONFIG"); env != "" {
		defaultKubeConfig = env
	} else if home := homedir.HomeDir(); home != "" {
		defaultKubeConfig = filepath.Join(home, ".kube", "config")
	}
	if defaultKubeConfig != "" {
		kubeconfig = flag.String("kubeconfig", defaultKubeConfig, "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	filename = flag.String("f", "", "(optional) filename to save Job resource")
	patchFile = flag.String("patch-file", "", "(optional) JSON file with patch information")
	flag.Usage = func() {
		fmt.Printf(`%[1]s - create custom job from cronjob template

Usage:
	%[1]s namespace name
	%[1]s namespace/name
	%[1]s name

Examples:
    # Edit a job interactively in your editor
    %[1]s namespace name
    
    # Apply patch from JSON or YAML file without opening an editor
    %[1]s --patch-file=/path/to/patch.json namespace name 
    
	# Patch file format :
	# Refer to https://kubernetes.io/docs/reference/kubectl/generated/kubectl_patch/


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

	var jobFilename string
	if filename == nil || *filename == "" {
		f, err := os.CreateTemp("", "kj.*.yaml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: failed to create temporary file: %v\n", cmdName, err)
			return exitStatusErr
		}
		jobFilename = f.Name()
		defer os.Remove(jobFilename)
	} else {
		jobFilename = *filename
	}

	var editor jobEditor
	// If patchFile is specified, skip interactive editing and apply the patch
	skipConfirm := false
	if patchFile != nil && *patchFile != "" {
		editor = &patchJobEditor{
			filename:  jobFilename,
			patchFile: *patchFile,
		}
		skipConfirm = true
	} else {
		editor = &interactiveJobEditor{
			filename: jobFilename,
		}
	}

	if err := editor.EditJob(job); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		return exitStatusErr
	}

	if !skipConfirm {
		confirmed, err := confirmByUser()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmdName, err)
		}
		if !confirmed {
			fmt.Println("canceled")
			return exitStatusOK
		}
	}

	if err := applyJob(jobFilename); err != nil {
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
	jobSpec, ownerRef, err := newJobTemplate(ctx, clientset, namespace, name)
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
			Namespace:       namespace,
			Name:            fmt.Sprintf("%s-%s", name, suffix),
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: jobSpec,
	}
	return job, nil
}

func newJobTemplate(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (jobSpec batchv1.JobSpec, ownerRef metav1.OwnerReference, err error) {
	v, err := clientset.ServerVersion()
	if err != nil {
		return jobSpec, ownerRef, fmt.Errorf("failed to get serverVersion: %w", err)
	}

	// When kubernetes version is 1.21 or higher, use batchv1.CronJob.
	// Otherwise, use batchv1beta1.CronJob
	if isCronJobGA(v) {
		cj, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return jobSpec, ownerRef, err
		}
		ownerRef := metav1.OwnerReference{
			APIVersion:         "batch/v1",
			Kind:               "CronJob",
			Name:               cj.GetName(),
			UID:                cj.GetUID(),
			BlockOwnerDeletion: toPtr(true),
		}
		return cj.Spec.JobTemplate.Spec, ownerRef, nil
	}

	cj, err := clientset.BatchV1beta1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return jobSpec, ownerRef, err
	}
	ownerRef = metav1.OwnerReference{
		APIVersion:         "batch/v1beta1",
		Kind:               "CronJob",
		Name:               cj.GetName(),
		UID:                cj.GetUID(),
		BlockOwnerDeletion: toPtr(true),
	}
	return cj.Spec.JobTemplate.Spec, ownerRef, nil
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

func confirmByUser() (bool, error) {
	tty, err := tty.Open()
	if err != nil {
		return false, err
	}
	defer tty.Close()
	fmt.Fprint(tty.Output(), "Do you want to create a job with the change you just made? [y/n]\n")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	defer signal.Stop(sigs)

	answerCh := make(chan string)
	errCh := make(chan error)

	go func() {
		for {
			answer, err := ttyutil.ReadLine(tty)
			if err != nil {
				if errors.Is(err, io.EOF) {
					continue
				}
				errCh <- err
				return
			}
			answerCh <- answer
		}
	}()

	for {
		select {
		case <-sigs:
			return false, nil
		case answer := <-answerCh:
			answer = strings.ToLower(strings.TrimSpace(answer))
			switch answer {
			case "y":
				return true, nil
			case "n", "":
				return false, nil
			default:
				fmt.Fprint(tty.Output(), "Please answer y or n: \n")
			}
		case err := <-errCh:
			return false, err
		}
	}
}

type jobEditor interface {
	EditJob(job *batchv1.Job) error
}
type interactiveJobEditor struct {
	filename string
}

func (e *interactiveJobEditor) EditJob(job *batchv1.Job) error {
	f, err := os.Create(e.filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := writeJobToFile(f, job); err != nil {
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
	editorWithArgs = append(editorWithArgs, e.filename)

	cmd := exec.Command(editorWithArgs[0], editorWithArgs[1:]...)
	cmd.Stdin = tty.Input()
	cmd.Stdout = tty.Output()
	cmd.Stderr = tty.Output()
	return cmd.Run()
}

func writeJobToFile(f *os.File, job *batchv1.Job) error {
	data, err := jobToYaml(job)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return f.Close()
}

type patchJobEditor struct {
	filename  string
	patchFile string
}

func (e *patchJobEditor) EditJob(job *batchv1.Job) error {
	patchBytes, err := os.ReadFile(e.patchFile)
	if err != nil {
		return fmt.Errorf("failed to read patch file: %w", err)
	}

	origData, err := jobToYaml(job)
	if err != nil {
		return err
	}

	origJSON, err := apiyaml.ToJSON(origData)
	if err != nil {
		return fmt.Errorf("failed to convert Job to JSON: %w", err)
	}

	patchJSON, err := apiyaml.ToJSON(patchBytes)
	if err != nil {
		return fmt.Errorf("failed to convert patch to JSON: %w\nPatch content: %s", err, string(patchBytes))
	}

	patchedJSON, err := strategicpatch.StrategicMergePatch(origJSON, patchJSON, job)
	if err != nil {
		return fmt.Errorf("failed to apply patch: %w", err)
	}

	patchedYAML, err := yaml.JSONToYAML(patchedJSON)
	if err != nil {
		return fmt.Errorf("failed to convert patched JSON to YAML: %w", err)
	}

	f, err := os.Create(e.filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(patchedYAML)
	return err
}

func applyJob(filename string) error {
	cmd := exec.Command("kubectl", "apply", "-f", filename)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func jobToYaml(job *batchv1.Job) ([]byte, error) {
	// Marshal with ownerReferences commented out
	ownerRefs, err := yaml.Marshal(map[string]any{"ownerReferences": job.ObjectMeta.OwnerReferences})
	if err != nil {
		return nil, err
	}

	job.ObjectMeta.OwnerReferences = nil
	data, err := yaml.Marshal(job)
	if err != nil {
		return nil, err
	}

	commentForOwnerRefs := "  # "
	commentedOwnerRefs := commentForOwnerRefs + strings.ReplaceAll(string(ownerRefs), "\n", "\n"+commentForOwnerRefs)
	commentedOwnerRefs = strings.TrimSuffix(commentedOwnerRefs, commentForOwnerRefs)
	namespaceInd := bytes.Index(data, []byte("namespace: "))
	namespaceLineInd := bytes.Index(data[namespaceInd:], []byte("\n")) + namespaceInd

	data = slices.Insert(data, namespaceLineInd+1, []byte(commentedOwnerRefs)...)
	return data, nil
}

func toPtr[T any](t T) *T {
	return &t
}
