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
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/kubectl/generate/versioned"
)

// amazonECRAuthenticationData is a wrapper for authentication data for an Amazon ECR registry.
type amazonECRAuthenticationData struct {
	server   string
	password string
	username string
}

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
func createOrUpdateSecret(k kubernetes.Interface, targetNamespace string, d *amazonECRAuthenticationData) error {
	// Create a 'Secret' object with the desired contents.
	v, err := (versioned.SecretForDockerRegistryGeneratorV1{
		Name:     d.server, // Use the server name as the name of the secret.
		Username: d.username,
		Email:    "none",
		Password: d.password,
		Server:   d.server,
	}).StructuredGenerate()
	if err != nil {
		return err
	}
	s := v.(*corev1.Secret)
	// Attempt to create the secret, falling back to updating it in case it already exists.
	log.Tracef(`Attempting to create secret "%s/%s"`, targetNamespace, s.Name)
	if _, err := k.CoreV1().Secrets(targetNamespace).Create(s); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Tracef(`Secret "%s/%s" already exists`, targetNamespace, s.Name)
			return updateSecret(k, targetNamespace, s)
		}
		return nil
	}
	log.Debugf(`Created secret "%s/%s"`, targetNamespace, s.Name)
	return nil
}

// getAmazonECRAuthenticationData returns authentication data for the target Amazon ECR registry.
func getAmazonECRAuthenticationData(e ecriface.ECRAPI) (*amazonECRAuthenticationData, error) {
	o, err := e.GetAuthorizationToken(&ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return nil, err
	}
	if len(o.AuthorizationData) != 1 {
		return nil, fmt.Errorf("expected a single result (got %d)", len(o.AuthorizationData))
	}
	s := strings.TrimPrefix(*o.AuthorizationData[0].ProxyEndpoint, "https://")
	v, err := base64.StdEncoding.DecodeString(*o.AuthorizationData[0].AuthorizationToken)
	if err != nil {
		return nil, err
	}
	t := strings.Split(string(v), ":")
	if len(t) != 2 {
		return nil, fmt.Errorf("aws returned a malformed token")
	}
	return &amazonECRAuthenticationData{server: s, password: t[1], username: t[0]}, nil
}

func main() {
	// Parse command-line flags.
	logLevel := flag.String("log-level", log.InfoLevel.String(), "the log level to use")
	pathToKubeconfig := flag.String("path-to-kubeconfig", "", "the path to the kubeconfig file to use")
	refreshInterval := flag.Duration("refresh-interval", time.Duration(12)*time.Hour, "the interval at which to refresh the secrets")
	targetNamespaces := flag.String("target-namespaces", corev1.NamespaceAll, "the comma-separated list of namespaces in which to create the secrets")
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

	// Initialize the Amazon ECR client.
	s, err := session.NewSession()
	if err != nil {
		log.Fatalf("Failed to initialize AWS session: %v", err)
	}
	e := ecr.New(s)

	// Setup a signal handler for SIGINT and SIGTERM so we can gracefully shutdown when requested to.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	// Refresh the target secrets everytime the specified refresh interval elapses.
	t := time.NewTicker(*refreshInterval)
	defer t.Stop()
	createOrUpdateSecrets(e, k, *targetNamespaces)
	for {
		select {
		case <-c:
			return
		case <-t.C:
			createOrUpdateSecrets(e, k, *targetNamespaces)
		}
	}
}

// createOrUpdateSecrets creates or updates secrets containing Docker credentials in each of the target namespaces.
func createOrUpdateSecrets(e ecriface.ECRAPI, k kubernetes.Interface, targetNamespaces string) {
	// Get the authorization token for the default registry
	d, err := getAmazonECRAuthenticationData(e)
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
	log.Tracef(`Attempting to update existing secret "%s/%s"`, targetNamespace, s.Name)
	v, err := k.CoreV1().Secrets(targetNamespace).Get(s.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	v.Data = s.Data
	if _, err := k.CoreV1().Secrets(v.Namespace).Update(v); err != nil {
		return err
	}
	log.Debugf(`Updated secret "%s/%s"`, v.Namespace, v.Name)
	return nil
}
