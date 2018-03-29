package main

import (
	"fmt"
	"os"

	nats "github.com/nats-io/go-nats"
	"k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func createVmJobSpec(kind string, backoffLimit int32, activeDeadlineSeconds int64) *v1.Job {
	charDev := apiv1.HostPathCharDev
	privileged := true
	return &v1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: kind + "-job",
		},
		Spec: v1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: kind + "-job",
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:       "buildkite-" + kind + "-builder",
							Image:      "tianon/qemu:latest",
							Command:    []string{"/vm/boot.sh", "/scratch/" + kind + "-hdd.img"},
							WorkingDir: "/vm",
							SecurityContext: &apiv1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: []apiv1.VolumeMount{
								{
									Name:      "nfs-" + kind,
									MountPath: "/vm",
								},
								{
									Name:      "dev-kvm",
									MountPath: "/dev/kvm",
								},
								{
									Name:      "scratch",
									MountPath: "/scratch",
								},
							},
						},
					},
					RestartPolicy: "Never",
					Volumes: []apiv1.Volume{
						{
							Name: "nfs-" + kind,
							VolumeSource: apiv1.VolumeSource{
								PersistentVolumeClaim: &apiv1.PersistentVolumeClaimVolumeSource{
									ClaimName: "nfs-" + kind,
									ReadOnly:  true,
								},
							},
						},
						{
							Name: "dev-kvm",
							VolumeSource: apiv1.VolumeSource{
								HostPath: &apiv1.HostPathVolumeSource{
									Path: "/dev/kvm",
									Type: &charDev,
								},
							},
						},
						{
							Name: "scratch",
							VolumeSource: apiv1.VolumeSource{
								HostPath: &apiv1.HostPathVolumeSource{
									Path: "/scratch",
								},
							},
						},
					},
				},
			},
		},
	}
}

func createContainerJobSpec(kind string, backoffLimit int32, activeDeadlineSeconds int64) *v1.Job {
	return &v1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: kind + "-job",
		},
		Spec: v1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: kind + "-job",
				},
				Spec: apiv1.PodSpec{
					RestartPolicy: "Never",
					Containers: []apiv1.Container{
						{
							Name:  "buildkite-ubuntu-builder",
							Image: "buildkite/agent:ubuntu",
							Env: []apiv1.EnvVar{
								{
									Name:  "BUILDKITE_AGENT_TOKEN",
									Value: os.Getenv("BUILDKITE_AGENT_TOKEN"),
								},
								{
									Name:  "BUILDKITE_AGENT_META_DATA",
									Value: "queue=ubuntu",
								},
							},
						},
					},
				},
			},
		},
	}
}

func createJobSpec(kind string) *v1.Job {
	var backoffLimit int32 = 1
	var activeDeadlineSeconds int64 = 600
	if vmJobType(kind) {
		return createVmJobSpec(kind, backoffLimit, activeDeadlineSeconds)
	}
	return createContainerJobSpec(kind, backoffLimit, activeDeadlineSeconds)
}

func vmJobType(msg string) bool {
	for _, v := range []string{"osx", "freebsd"} {
		if msg == v {
			return true
		}
	}
	return false
}

func validJobType(msg string) bool {
	for _, v := range []string{"osx", "freebsd", "ubuntu"} {
		if msg == v {
			return true
		}
	}
	return false
}

func main() {

	// home, _ := os.LookupEnv("HOME")
	// kubeconfig := filepath.Join(home, ".kube", "config")
	// // use the current context in kubeconfig
	// config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	// if err != nil {
	// 	panic(err.Error())
	// }

	natsHost, ok := os.LookupEnv("NATSHOST")
	if !ok {
		panic("No NATSHOST env var set")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	jobsClient := clientset.BatchV1().Jobs(apiv1.NamespaceDefault)

	nc, _ := nats.Connect(natsHost)
	defer nc.Close()

	ch := make(chan *nats.Msg)
	nc.QueueSubscribeSyncWithChan("builds.start", "bq", ch)
	for msg := range ch {
		data := string(msg.Data)
		fmt.Printf("got msg: '%s'\n", data)
		if validJobType(data) {
			fmt.Printf("creating job '%s'...\n", data)
			_, err := jobsClient.Create(createJobSpec(data))
			if err != nil {
				fmt.Printf("error creating job '%s': %+v\n", data, err)
				continue
			}
			fmt.Printf("created job: %s\n", data)
		}
	}
}
