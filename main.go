// Copyright 2019 Form3 Financial Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/kubectl/pkg/generate/versioned"

	"github.com/form3tech-oss/kube-ecr-refresher/internal/refresher"
)

// buildNamespacesList takes a comma-separated list of namespace names (or "") and converts that into a list of namespace names.
func buildNamespacesList(k kubernetes.Interface, targetNamespaces string) ([]string, error) {
	if targetNamespaces != corev1.NamespaceAll {
		return strings.Split(targetNamespaces, ","), nil
	}
	l, err := k.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	r := make([]string, 0, len(l.Items))
	for _, n := range l.Items {
		r = append(r, n.GetName())
	}
	return r, nil
}

// createKubeClient creates a Kubernetes client based on the specified kubeconfig file.
func createKubeClient(pathToKubeconfig string) (kubernetes.Interface, error) {
	c, err := clientcmd.BuildConfigFromFlags("", pathToKubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(c)
}

// createOrUpdateSecret creates or updates a secret in the specified namespace containing the required Docker credentials.
func createOrUpdateSecret(k kubernetes.Interface, targetNamespace string, d *refresher.AmazonECRAuthenticationData) error {
	// Create a 'Secret' object with the desired contents.
	v, err := (versioned.SecretForDockerRegistryGeneratorV1{
		Name:     d.Server, // Use the server name as the name of the secret.
		Username: d.Username,
		Email:    "none",
		Password: d.Password,
		Server:   d.Server,
	}).StructuredGenerate()
	if err != nil {
		return err
	}
	s := v.(*corev1.Secret)
	// Attempt to create the secret, falling back to updating it in case it already exists.
	log.Debugf(`Attempting to create secret "%s/%s"`, targetNamespace, s.Name)
	if _, err := k.CoreV1().Secrets(targetNamespace).Create(s); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Debugf(`Secret "%s/%s" already exists`, targetNamespace, s.Name)
			return updateSecret(k, targetNamespace, s)
		}
		return nil
	}
	log.Debugf(`Created secret "%s/%s"`, targetNamespace, s.Name)
	return nil
}

func main() {
	// Parse command-line flags.
	logLevel := flag.String("log-level", log.InfoLevel.String(), "the log level to use")
	pathToKubeconfig := flag.String("path-to-kubeconfig", "", "the path to the kubeconfig file to use")
	refreshInterval := flag.Duration("refresh-interval", time.Duration(10)*time.Second, "the interval at which to refresh the list of namespaces and create/update secrets")
	targetNamespaces := flag.String("target-namespaces", corev1.NamespaceAll, "the comma-separated list of namespaces in which to create/update secrets")
	flag.Parse()

	// Configure logging.
	if v, err := log.ParseLevel(*logLevel); err != nil {
		log.Fatalf("Failed to parse log level: %v", err)
	} else {
		log.SetLevel(v)
	}
	klog.SetOutput(ioutil.Discard)

	// Create a Kubernetes configuration object and client.
	k, err := createKubeClient(*pathToKubeconfig)
	if err != nil {
		log.Fatalf("Failed to build Kubernetes client: %v", err)
	}

	// Create and start an Amazon ECR authentication data refresher.
	r, err := refresher.New()
	if err != nil {
		log.Fatalf("Failed to build Amazon ECR authentication data refresher: %v", err)
	}
	go r.Run()

	// Wait until the Amazon ECR authentication data is first refreshed.
	for {
		if _, err := r.Get(); err == nil {
			break
		}
		log.Debugf("Waiting for Amazon ECR authentication data to be refreshed")
		time.Sleep(5 * time.Second)
	}

	// Setup a signal handler for SIGINT and SIGTERM so we can gracefully shutdown when requested to.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	// Refresh the target secrets every time the specified refresh interval elapses.
	t := time.NewTicker(*refreshInterval)
	defer t.Stop()
	createOrUpdateSecrets(r, k, *targetNamespaces)
	for {
		select {
		case <-c:
			return
		case <-t.C:
			createOrUpdateSecrets(r, k, *targetNamespaces)
		}
	}
}

// createOrUpdateSecrets creates or updates secrets containing Docker credentials in each of the target namespaces.
func createOrUpdateSecrets(r *refresher.AmazonECRAuthenticationDataRefresher, k kubernetes.Interface, targetNamespaces string) {
	// Get the authorization data from the Amazon ECR authentication data refresher.
	d, err := r.Get()
	if err != nil {
		log.Errorf("Failed to get Amazon ECR authentication data: %v", err)
		return
	}
	// Build the list of target Kubernetes namespaces.
	l, err := buildNamespacesList(k, targetNamespaces)
	if err != nil {
		log.Errorf("Failed to list Kubernetes namespaces: %v", err)
		return
	}
	// Create or update the secret in each of the target namespaces.
	var w sync.WaitGroup
	for _, n := range l {
		w.Add(1)
		go func(n string) {
			defer w.Done()
			if err := createOrUpdateSecret(k, n, d); err != nil {
				log.Errorf("Failed to create or update secret in Kubernetes namespace %q: %v", n, err)
				return
			}
		}(n)
	}
	w.Wait()
}

// updateSecret updates the target secret with the updated Docker credentials.
func updateSecret(k kubernetes.Interface, targetNamespace string, s *corev1.Secret) error {
	log.Debugf(`Attempting to update existing secret "%s/%s"`, targetNamespace, s.Name)
	v, err := k.CoreV1().Secrets(targetNamespace).Get(s.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if reflect.DeepEqual(v.Data, s.Data) {
		log.Debugf(`Secret "%s/%s" is up-to-date`, v.Namespace, v.Name)
		return nil
	}
	v.Data = s.Data
	if _, err := k.CoreV1().Secrets(v.Namespace).Update(v); err != nil {
		return err
	}
	log.Debugf(`Updated secret "%s/%s"`, v.Namespace, v.Name)
	return nil
}
